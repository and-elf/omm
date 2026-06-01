package ubus

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Client interface {
	Call(ctx context.Context, object, method string, params interface{}, result interface{}) error
	Close() error
}

type Options struct {
	SocketPath string
	BinaryPath string
}

type client struct {
	socketPath string
	binaryPath string
}

func NewClient(opts Options) (Client, error) {
	binaryPath := opts.BinaryPath
	if binaryPath == "" {
		binaryPath = "ubus"
	}

	return &client{
		socketPath: opts.SocketPath,
		binaryPath: binaryPath,
	}, nil
}

func (c *client) Call(ctx context.Context, object, method string, params interface{}, result interface{}) error {
	if params == nil {
		params = map[string]interface{}{}
	}

	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal ubus params: %w", err)
	}

	args := []string{}
	if c.socketPath != "" {
		args = append(args, "-s", c.socketPath)
	}
	args = append(args, "call", object, method, string(payload))

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ubus call %s.%s: %w: %s", object, method, err, strings.TrimSpace(string(output)))
	}

	if result != nil {
		if err := json.Unmarshal(output, result); err != nil {
			return fmt.Errorf("parse ubus response: %w", err)
		}
	}

	return nil
}

func (c *client) Close() error {
	return nil
}
