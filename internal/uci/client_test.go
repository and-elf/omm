package uci

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type ubusCall struct {
	object string
	method string
}

type fakeUbusClient struct {
	lastObject string
	lastMethod string
	lastParams interface{}
	calls      []ubusCall
	response   interface{}
	err        error
	errOn      string // "object.method" that should return errOnErr
	errOnErr   error
}

func (f *fakeUbusClient) Call(ctx context.Context, object, method string, params interface{}, result interface{}) error {
	f.lastObject = object
	f.lastMethod = method
	f.lastParams = params
	f.calls = append(f.calls, ubusCall{object: object, method: method})
	if f.errOn != "" && f.errOn == object+"."+method {
		return f.errOnErr
	}
	if f.err != nil {
		return f.err
	}
	if result == nil || f.response == nil {
		return nil
	}
	payload, err := json.Marshal(f.response)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, result)
}

func (f *fakeUbusClient) Close() error {
	return nil
}

func TestGet(t *testing.T) {
	client := &client{ubusClient: &fakeUbusClient{response: map[string]interface{}{"values": map[string]interface{}{"hostname": "OpenWrt"}}}}

	value, err := client.Get(context.Background(), "system", "@system[0]", "hostname")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "OpenWrt" {
		t.Fatalf("expected OpenWrt, got %q", value)
	}
}

func TestSet(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	if err := client.Set(context.Background(), "wireless", "mesh", "ssid", "foo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastObject != "uci" || fake.lastMethod != "set" {
		t.Fatalf("expected uci set call, got %s.%s", fake.lastObject, fake.lastMethod)
	}

	params, ok := fake.lastParams.(map[string]interface{})
	if !ok {
		t.Fatalf("expected params map, got %T", fake.lastParams)
	}
	if params["config"] != "wireless" || params["section"] != "mesh" {
		t.Fatalf("unexpected params: %#v", params)
	}
	values, ok := params["values"].(map[string]string)
	if !ok {
		converted, ok := params["values"].(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected values type: %#v", params["values"])
		}
		values = make(map[string]string)
		for k, v := range converted {
			values[k] = v.(string)
		}
	}
	if values["ssid"] != "foo" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestCommit(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	if err := client.Commit(context.Background(), "wireless"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastObject != "uci" || fake.lastMethod != "commit" {
		t.Fatalf("expected uci commit call, got %s.%s", fake.lastObject, fake.lastMethod)
	}
	if !reflect.DeepEqual(fake.lastParams, map[string]string{"config": "wireless"}) {
		t.Fatalf("unexpected params: %#v", fake.lastParams)
	}
}

func TestSetSection(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	// Creating a typed, named section (e.g. a wifi-iface) is an `add` (rpcd's
	// `set` won't create a missing section) followed by a `set` that carries the
	// option values.
	err := client.SetSection(context.Background(), "wireless", "omm_setup", "wifi-iface", map[string]string{
		"mode": "ap",
		"ssid": "OMM-Setup-abcd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []ubusCall{{object: "uci", method: "add"}, {object: "uci", method: "set"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("expected uci add then uci set, got %#v", fake.calls)
	}

	// The trailing `set` carries the option values on the named section.
	params, ok := fake.lastParams.(map[string]interface{})
	if !ok {
		t.Fatalf("expected params map, got %T", fake.lastParams)
	}
	if params["config"] != "wireless" || params["section"] != "omm_setup" {
		t.Fatalf("unexpected set params: %#v", params)
	}
	values, ok := params["values"].(map[string]string)
	if !ok {
		t.Fatalf("unexpected values type: %#v", params["values"])
	}
	if values["mode"] != "ap" || values["ssid"] != "OMM-Setup-abcd" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestDelete(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	if err := client.Delete(context.Background(), "network", "ommsetup"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastObject != "uci" || fake.lastMethod != "delete" {
		t.Fatalf("expected uci delete call, got %s.%s", fake.lastObject, fake.lastMethod)
	}
	if !reflect.DeepEqual(fake.lastParams, map[string]string{"config": "network", "section": "ommsetup"}) {
		t.Fatalf("unexpected params: %#v", fake.lastParams)
	}
}

func TestSections(t *testing.T) {
	fake := &fakeUbusClient{response: map[string]interface{}{
		"values": map[string]interface{}{
			"radio0": map[string]interface{}{".type": "wifi-device", "band": "5g"},
			// A list-valued option must be skipped, not error the whole call.
			"@wifi-iface[0]": map[string]interface{}{".type": "wifi-iface", "list": []string{"a", "b"}},
		},
	}}
	client := &client{ubusClient: fake}

	got, err := client.Sections(context.Background(), "wireless")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["radio0"][".type"] != "wifi-device" || got["radio0"]["band"] != "5g" {
		t.Fatalf("radio0 options not parsed: %#v", got["radio0"])
	}
	if _, present := got["@wifi-iface[0]"]["list"]; present {
		t.Fatalf("list-valued option should be skipped, got %#v", got["@wifi-iface[0]"])
	}
	if fake.lastObject != "uci" || fake.lastMethod != "get" {
		t.Fatalf("expected uci.get, got %s.%s", fake.lastObject, fake.lastMethod)
	}
}

// The dhcp service event is best-effort: environments without procd (e.g. a
// minimal ubusd+rpcd test container) lack the `service` object, and that must
// not fail the reload — the config is already committed and netifd reloaded.
func TestReloadDhcpEventIsBestEffort(t *testing.T) {
	fake := &fakeUbusClient{errOn: "service.event", errOnErr: errors.New("Object not found")}
	client := &client{ubusClient: fake}

	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("reload must succeed despite a failing dhcp service event, got: %v", err)
	}
	// The network/wireless reloads still ran, and the event was attempted.
	want := []ubusCall{
		{object: "network", method: "reload"},
		{object: "network.wireless", method: "reconf"},
		{object: "service", method: "event"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("unexpected call sequence: %#v", fake.calls)
	}
}

func TestReload(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []ubusCall{
		{object: "network", method: "reload"},
		{object: "network.wireless", method: "reconf"},
		{object: "service", method: "event"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("expected network reload, wireless reconf, then dhcp service event, got %#v", fake.calls)
	}

	// The service event must be a procd config.change for the dhcp package, so
	// dnsmasq/odhcpd re-read config and serve the freshly-committed pool.
	// Without it, a client associates to the setup AP but never gets a lease.
	want_params := map[string]interface{}{
		"type": "config.change",
		"data": map[string]string{"package": "dhcp"},
	}
	if !reflect.DeepEqual(fake.lastParams, want_params) {
		t.Fatalf("dhcp reload event params = %#v, want %#v", fake.lastParams, want_params)
	}
}

// AddListItem/DelListItem must not use the ubus uci `add_list`/`del_list`
// methods: rpcd's uci object does not implement them ("Method not found" on the
// live devices). They read the current list and write it back with `set` (whose
// values may be an array), which every rpcd supports.
func listResponse(items ...string) map[string]interface{} {
	vals := make([]interface{}, len(items))
	for i, it := range items {
		vals[i] = it
	}
	return map[string]interface{}{"values": map[string]interface{}{"ports": vals}}
}

func setValues(t *testing.T, params interface{}) []string {
	t.Helper()
	m, ok := params.(map[string]interface{})
	if !ok {
		t.Fatalf("expected params map, got %T", params)
	}
	values, ok := m["values"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected values map, got %T", m["values"])
	}
	out, ok := values["ports"].([]string)
	if !ok {
		t.Fatalf("expected ports []string, got %T", values["ports"])
	}
	return out
}

func TestAddListItemAppendsViaSet(t *testing.T) {
	fake := &fakeUbusClient{response: listResponse("lan1", "lan2")}
	client := &client{ubusClient: fake}

	if err := client.AddListItem(context.Background(), "network", "@device[0]", "ports", "bat0"); err != nil {
		t.Fatalf("AddListItem: %v", err)
	}
	for _, c := range fake.calls {
		if c.method == "add_list" || c.method == "del_list" {
			t.Fatalf("used unsupported method %q", c.method)
		}
	}
	if got := setValues(t, fake.lastParams); !reflect.DeepEqual(got, []string{"lan1", "lan2", "bat0"}) {
		t.Fatalf("ports = %v, want [lan1 lan2 bat0]", got)
	}
}

func TestAddListItemIdempotent(t *testing.T) {
	fake := &fakeUbusClient{response: listResponse("lan1", "bat0")}
	client := &client{ubusClient: fake}

	if err := client.AddListItem(context.Background(), "network", "@device[0]", "ports", "bat0"); err != nil {
		t.Fatalf("AddListItem: %v", err)
	}
	// Already present: read only, no write.
	for _, c := range fake.calls {
		if c.method == "set" {
			t.Fatalf("AddListItem wrote despite item already present")
		}
	}
}

func TestDelListItemRemovesViaSet(t *testing.T) {
	fake := &fakeUbusClient{response: listResponse("lan1", "bat0")}
	client := &client{ubusClient: fake}

	if err := client.DelListItem(context.Background(), "network", "@device[0]", "ports", "bat0"); err != nil {
		t.Fatalf("DelListItem: %v", err)
	}
	for _, c := range fake.calls {
		if c.method == "add_list" || c.method == "del_list" {
			t.Fatalf("used unsupported method %q", c.method)
		}
	}
	if got := setValues(t, fake.lastParams); !reflect.DeepEqual(got, []string{"lan1"}) {
		t.Fatalf("ports = %v, want [lan1]", got)
	}
}

func TestDelListItemAbsentNoOp(t *testing.T) {
	fake := &fakeUbusClient{response: listResponse("lan1", "lan2")}
	client := &client{ubusClient: fake}

	if err := client.DelListItem(context.Background(), "network", "@device[0]", "ports", "bat0"); err != nil {
		t.Fatalf("DelListItem: %v", err)
	}
	for _, c := range fake.calls {
		if c.method == "set" {
			t.Fatalf("DelListItem wrote despite item absent")
		}
	}
}

// Removing the last member of a list must clear the option via uci `delete`:
// rpcd's `set` with an empty array is a no-op, which would leave the removed
// member behind (the live br-lan `ports='bat0'` leftover after a batman teardown).
func TestDelListItemLastMemberClearsOption(t *testing.T) {
	fake := &fakeUbusClient{response: listResponse("bat0")}
	client := &client{ubusClient: fake}

	if err := client.DelListItem(context.Background(), "network", "@device[0]", "ports", "bat0"); err != nil {
		t.Fatalf("DelListItem: %v", err)
	}
	last := fake.calls[len(fake.calls)-1]
	if last.method != "delete" {
		t.Fatalf("emptying a list should issue uci delete of the option, got %q", last.method)
	}
	params, _ := fake.lastParams.(map[string]string)
	if params["option"] != "ports" || params["section"] != "@device[0]" {
		t.Fatalf("unexpected delete params: %#v", fake.lastParams)
	}
}
