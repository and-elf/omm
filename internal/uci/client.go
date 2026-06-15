package uci

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/and-elf/omm/internal/ubus"
)

type Options struct {
	SocketPath string
	BinaryPath string
}

type Client interface {
	Get(ctx context.Context, packageName, section, option string) (string, error)
	// Sections returns every section of a config as name -> options. The option
	// map includes the pseudo-keys ".type" and ".name". List-valued options are
	// omitted (only scalar string values are returned), which is all the
	// callers here need.
	Sections(ctx context.Context, packageName string) (map[string]map[string]string, error)
	Set(ctx context.Context, packageName, section, option, value string) error
	// SetSection creates or updates a named section of the given type and sets
	// all of its option values in one call. Use it to author a section that does
	// not exist yet (e.g. a wifi-iface or a dhcp pool); a plain Set only updates
	// an option on an existing section.
	SetSection(ctx context.Context, packageName, section, sectionType string, values map[string]string) error
	// Delete removes an entire named section from a config.
	Delete(ctx context.Context, packageName, section string) error
	// AddListItem appends value to a list-valued option (UCI `add_list`), e.g. a
	// bridge device's `ports`. Idempotent in rpcd: adding an existing value is a
	// no-op.
	AddListItem(ctx context.Context, packageName, section, option, value string) error
	// DelListItem removes value from a list-valued option (UCI `del_list`).
	DelListItem(ctx context.Context, packageName, section, option, value string) error
	Commit(ctx context.Context, packageName string) error
	// Reload applies committed configuration to the running system. A bare
	// `uci commit` only rewrites the config files; netifd has to be told to
	// reconfigure before the changes take effect.
	Reload(ctx context.Context) error
	Close() error
}

type client struct {
	ubusClient ubus.Client
}

func NewClient(opts Options) (Client, error) {
	ubusClient, err := ubus.NewClient(ubus.Options{SocketPath: opts.SocketPath, BinaryPath: opts.BinaryPath})
	if err != nil {
		return nil, err
	}
	return &client{ubusClient: ubusClient}, nil
}

func (c *client) Close() error {
	return c.ubusClient.Close()
}

type uciGetResult struct {
	Values map[string]json.RawMessage `json:"values"`
}

func (c *client) Get(ctx context.Context, packageName, section, option string) (string, error) {
	params := map[string]string{
		"config":  packageName,
		"section": section,
	}
	if option != "" {
		params["option"] = option
	}

	var result uciGetResult
	if err := c.ubusClient.Call(ctx, "uci", "get", params, &result); err != nil {
		return "", err
	}

	value, ok := result.Values[option]
	if !ok {
		return "", fmt.Errorf("option %q not found", option)
	}

	var str string
	if err := json.Unmarshal(value, &str); err != nil {
		return "", fmt.Errorf("parse uci value: %w", err)
	}

	return str, nil
}

type uciSectionsResult struct {
	Values map[string]map[string]json.RawMessage `json:"values"`
}

func (c *client) Sections(ctx context.Context, packageName string) (map[string]map[string]string, error) {
	var result uciSectionsResult
	if err := c.ubusClient.Call(ctx, "uci", "get", map[string]string{"config": packageName}, &result); err != nil {
		return nil, err
	}

	out := make(map[string]map[string]string, len(result.Values))
	for name, options := range result.Values {
		opts := make(map[string]string, len(options))
		for key, raw := range options {
			// Keep only scalar string options; list values (e.g. `list`) fail
			// to unmarshal into a string and are skipped.
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				opts[key] = s
			}
		}
		out[name] = opts
	}
	return out, nil
}

func (c *client) Set(ctx context.Context, packageName, section, option, value string) error {
	params := map[string]interface{}{
		"config":  packageName,
		"section": section,
		"values":  map[string]string{option: value},
	}
	return c.ubusClient.Call(ctx, "uci", "set", params, nil)
}

func (c *client) SetSection(ctx context.Context, packageName, section, sectionType string, values map[string]string) error {
	// rpcd's uci `set` only updates an existing section (it returns "Not found"
	// for a missing one), so first `add` the named section of the requested type
	// — idempotent: a repeat add of an existing named section is a no-op — then
	// `set` its option values in one call.
	addParams := map[string]interface{}{
		"config": packageName,
		"type":   sectionType,
		"name":   section,
	}
	if err := c.ubusClient.Call(ctx, "uci", "add", addParams, nil); err != nil {
		return err
	}
	setParams := map[string]interface{}{
		"config":  packageName,
		"section": section,
		"values":  values,
	}
	return c.ubusClient.Call(ctx, "uci", "set", setParams, nil)
}

func (c *client) Delete(ctx context.Context, packageName, section string) error {
	params := map[string]string{
		"config":  packageName,
		"section": section,
	}
	return c.ubusClient.Call(ctx, "uci", "delete", params, nil)
}

// AddListItem appends value to a list-valued option, idempotently. rpcd's uci
// ubus object has no `add_list` method (it returns "Method not found"), so we
// read the current list and write it back with `set` — whose `values` may carry
// an array — which every rpcd supports. A value already present is a no-op.
func (c *client) AddListItem(ctx context.Context, packageName, section, option, value string) error {
	list, err := c.getList(ctx, packageName, section, option)
	if err != nil {
		return err
	}
	for _, v := range list {
		if v == value {
			return nil
		}
	}
	return c.setList(ctx, packageName, section, option, append(list, value))
}

// DelListItem removes value from a list-valued option. Like AddListItem it uses
// read-modify-write via `set` rather than the unsupported `del_list`. Removing
// an absent value is a no-op.
func (c *client) DelListItem(ctx context.Context, packageName, section, option, value string) error {
	list, err := c.getList(ctx, packageName, section, option)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(list))
	removed := false
	for _, v := range list {
		if v == value {
			removed = true
			continue
		}
		out = append(out, v)
	}
	if !removed {
		return nil
	}
	return c.setList(ctx, packageName, section, option, out)
}

// getList reads a list-valued option as a slice, returning an empty slice when
// the option is unset. A scalar value is returned as a single-element slice.
func (c *client) getList(ctx context.Context, packageName, section, option string) ([]string, error) {
	var result uciGetResult
	if err := c.ubusClient.Call(ctx, "uci", "get", map[string]string{
		"config":  packageName,
		"section": section,
		"option":  option,
	}, &result); err != nil {
		return nil, err
	}
	raw, ok := result.Values[option]
	if !ok {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var scalar string
	if err := json.Unmarshal(raw, &scalar); err == nil {
		return []string{scalar}, nil
	}
	return nil, fmt.Errorf("parse uci list %q", option)
}

// setList writes a list-valued option in one `uci set` call, passing the values
// as an array (rpcd sets a list option when the value is an array). An empty
// list is written by deleting the option: `set` with an empty array is a no-op
// on rpcd, which would otherwise leave the last removed member behind.
func (c *client) setList(ctx context.Context, packageName, section, option string, values []string) error {
	if len(values) == 0 {
		return c.ubusClient.Call(ctx, "uci", "delete", map[string]string{
			"config":  packageName,
			"section": section,
			"option":  option,
		}, nil)
	}
	return c.ubusClient.Call(ctx, "uci", "set", map[string]interface{}{
		"config":  packageName,
		"section": section,
		"values":  map[string]interface{}{option: values},
	}, nil)
}

func (c *client) Commit(ctx context.Context, packageName string) error {
	params := map[string]string{
		"config": packageName,
	}
	return c.ubusClient.Call(ctx, "uci", "commit", params, nil)
}

// Reload re-applies committed config to the running system: netifd reloads the
// network interfaces, the radios are reconfigured, and a procd config.change
// event makes the DHCP services re-read their config.
//
// The dhcp event is what lets the setup AP hand out leases: netifd brings the
// interface up, but the DHCP server (dnsmasq, plus odhcpd) only serves a
// freshly-committed pool after it is told to reload — exactly what
// `/sbin/reload_config` emits on a `uci commit`. Without it a client associates
// to the AP but never gets a lease. The radios use bind-dynamic, so dnsmasq
// picks up the setup interface even though netifd brings it up asynchronously
// after this returns.
//
// The dhcp event is best-effort: the `service` object is provided by procd, so
// on a real device it is always present, but minimal environments (e.g. an
// ubusd+rpcd test container without procd) lack it. A failure there must not
// fail the whole reload — the config is already committed and netifd has been
// reloaded — so the error is logged and swallowed.
func (c *client) Reload(ctx context.Context) error {
	if err := c.ubusClient.Call(ctx, "network", "reload", nil, nil); err != nil {
		return fmt.Errorf("reload network: %w", err)
	}
	if err := c.ubusClient.Call(ctx, "network.wireless", "reconf", nil, nil); err != nil {
		return fmt.Errorf("reconf wireless: %w", err)
	}
	dhcpEvent := map[string]interface{}{
		"type": "config.change",
		"data": map[string]string{"package": "dhcp"},
	}
	if err := c.ubusClient.Call(ctx, "service", "event", dhcpEvent, nil); err != nil {
		log.Printf("uci reload: dhcp service event failed (non-fatal): %v", err)
	}
	return nil
}
