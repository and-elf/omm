package uci

import (
	"context"
	"encoding/json"
	"fmt"

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
// The dhcp event is essential for the setup AP: netifd brings the interface up,
// but the DHCP server (dnsmasq, plus odhcpd) only serves a freshly-committed
// pool after it is told to reload — exactly what `/sbin/reload_config` emits on
// a `uci commit`. Without it a client associates to the AP but never gets a
// lease. The radios use bind-dynamic, so dnsmasq picks up the setup interface
// even though netifd brings it up asynchronously after this returns.
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
		return fmt.Errorf("reload dhcp services: %w", err)
	}
	return nil
}
