package uci

import (
	"context"
	"encoding/json"
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
}

func (f *fakeUbusClient) Call(ctx context.Context, object, method string, params interface{}, result interface{}) error {
	f.lastObject = object
	f.lastMethod = method
	f.lastParams = params
	f.calls = append(f.calls, ubusCall{object: object, method: method})
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

func TestReload(t *testing.T) {
	fake := &fakeUbusClient{}
	client := &client{ubusClient: fake}

	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []ubusCall{
		{object: "network", method: "reload"},
		{object: "network.wireless", method: "reconf"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("expected network reload then wireless reconf, got %#v", fake.calls)
	}
}
