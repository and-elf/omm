package ubus

import (
	"context"
	"testing"
)

// A request context that is already cancelled must NOT stop the ubus call from
// running. The rpcd plugin's busybox-nc transport half-closes the connection
// once the request is written, which cancels the HTTP request context; the
// downstream `ubus` work (topology, profile apply) has to survive that, or it
// dies with "context canceled" before meshd can answer.
func TestCallSurvivesCancelledContext(t *testing.T) {
	// `true` ignores its args and exits 0 — a stand-in for the ubus binary.
	c, err := NewClient(Options{BinaryPath: "true"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled, as on a client half-close

	if err := c.Call(ctx, "uci", "commit", map[string]string{"config": "wireless"}, nil); err != nil {
		t.Fatalf("call must run despite a cancelled context, got: %v", err)
	}
}

// A non-zero exit from the ubus binary is still surfaced as an error.
func TestCallReportsCommandFailure(t *testing.T) {
	c, _ := NewClient(Options{BinaryPath: "false"})
	if err := c.Call(context.Background(), "uci", "add", nil, nil); err == nil {
		t.Fatal("expected an error when the ubus binary exits non-zero")
	}
}
