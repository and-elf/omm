package ubus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// execTimeout bounds a single ubus invocation. ubus calls here (uci ops,
// netifd reloads, hostapd reads) all return quickly; this is a backstop, not a
// normal limit.
const execTimeout = 30 * time.Second

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

	// Detach the subprocess from the caller's cancellation. The rpcd plugin
	// reaches meshd over a busybox-nc connection that half-closes once the
	// request is written; Go then cancels the HTTP request context. Tying the
	// `ubus` exec to that context would kill in-flight work (topology reads,
	// profile apply) with "context canceled" the instant the client's write
	// side closes — even though meshd can still send its response. A short
	// timeout keeps a genuinely stuck call from hanging forever.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), execTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, c.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ubus call %s.%s: %w: %s", object, method, err, strings.TrimSpace(string(output)))
	}

	if result != nil {
		// rpcd answers with an empty body (exit 0, no stdout) when a call has no
		// payload to return — e.g. `uci get` of an option or section that is
		// unset. Leave result at its zero value rather than failing to parse ""
		// as JSON, which otherwise makes every read of an absent option fail with
		// "unexpected end of JSON input".
		if len(bytes.TrimSpace(output)) == 0 {
			return nil
		}
		if err := json.Unmarshal(output, result); err != nil {
			return fmt.Errorf("parse ubus response: %w", err)
		}
	}

	return nil
}

func (c *client) Close() error {
	return nil
}
