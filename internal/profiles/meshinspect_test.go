package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeUbus returns canned JSON for the network.wireless status call (or an
// error), so MeshUp is tested without a live ubus.
type fakeUbus struct {
	json string
	err  error
}

func (f fakeUbus) Call(ctx context.Context, object, method string, params, result interface{}) error {
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(f.json), result)
}

func (f fakeUbus) Close() error { return nil }

func TestUbusMeshInspector(t *testing.T) {
	const meshSec = "omm_mesh"
	tests := []struct {
		name    string
		status  string
		want    bool
		wantErr bool
	}{
		{
			name:   "mesh up: radio up, iface has ifname",
			status: `{"radio0":{"up":true,"retry_setup_failed":false,"interfaces":[{"section":"omm_ap","ifname":"wlan0"},{"section":"omm_mesh","ifname":"wlan0-mesh"}]}}`,
			want:   true,
		},
		{
			name:   "mesh down: radio setup failed",
			status: `{"radio0":{"up":true,"retry_setup_failed":true,"interfaces":[{"section":"omm_mesh","ifname":""}]}}`,
			want:   false,
		},
		{
			name:   "mesh down: no ifname assigned",
			status: `{"radio0":{"up":true,"retry_setup_failed":false,"interfaces":[{"section":"omm_mesh","ifname":""}]}}`,
			want:   false,
		},
		{
			name:   "mesh down: section absent (never instantiated)",
			status: `{"radio0":{"up":true,"interfaces":[{"section":"omm_ap","ifname":"wlan0"}]}}`,
			want:   false,
		},
		{
			name:   "radio down",
			status: `{"radio0":{"up":false,"interfaces":[{"section":"omm_mesh","ifname":"wlan0-mesh"}]}}`,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insp := UbusMeshInspector{Ubus: fakeUbus{json: tt.status}}
			got, err := insp.MeshUp(context.Background(), meshSec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("MeshUp=%v want %v", got, tt.want)
			}
		})
	}
}

func TestUbusMeshInspectorPropagatesError(t *testing.T) {
	insp := UbusMeshInspector{Ubus: fakeUbus{err: errors.New("ubus down")}}
	if _, err := insp.MeshUp(context.Background(), "omm_mesh"); err == nil {
		t.Fatal("expected ubus error to propagate")
	}
}
