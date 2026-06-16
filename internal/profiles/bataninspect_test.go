package profiles

import (
	"context"
	"errors"
	"testing"
)

func TestUbusBatmanInspector(t *testing.T) {
	tests := []struct {
		name string
		json string
		err  error
		want bool
	}{
		{name: "bat0 present and up", json: `{"up":true,"present":true}`, want: true},
		{name: "bat0 present, no carrier yet", json: `{"up":false,"present":true}`, want: true},
		{name: "device absent (error)", err: errors.New("Not found"), want: false},
		{name: "present false", json: `{"up":false,"present":false}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insp := UbusBatmanInspector{Ubus: fakeUbus{json: tt.json, err: tt.err}}
			got, err := insp.BatmanUp(context.Background(), "bat0")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("BatmanUp=%v want %v", got, tt.want)
			}
		})
	}
}
