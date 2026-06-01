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
	Set(ctx context.Context, packageName, section, option, value string) error
	Commit(ctx context.Context, packageName string) error
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

func (c *client) Set(ctx context.Context, packageName, section, option, value string) error {
	params := map[string]interface{}{
		"config":  packageName,
		"section": section,
		"values":  map[string]string{option: value},
	}
	return c.ubusClient.Call(ctx, "uci", "set", params, nil)
}

func (c *client) Commit(ctx context.Context, packageName string) error {
	params := map[string]string{
		"config": packageName,
	}
	return c.ubusClient.Call(ctx, "uci", "commit", params, nil)
}
