package topology

import (
	"context"
	"errors"
	"testing"
)

// fakeFS maps sysfs paths to their contents (or an error) for backhaul tests.
type fakeFS map[string]struct {
	data []byte
	err  error
}

func (f fakeFS) read(path string) ([]byte, error) {
	if e, ok := f[path]; ok {
		return e.data, e.err
	}
	return nil, errors.New("no such file")
}

func TestSysfsBackhaulClassifies(t *testing.T) {
	const iface = "eth0"
	carrier := "/sys/class/net/eth0/carrier"
	operstate := "/sys/class/net/eth0/operstate"

	tests := []struct {
		name string
		fs   fakeFS
		want string
	}{
		{
			name: "carrier up => ethernet",
			fs:   fakeFS{carrier: {data: []byte("1\n")}},
			want: BackhaulEthernet,
		},
		{
			name: "carrier down => wireless",
			fs:   fakeFS{carrier: {data: []byte("0\n")}},
			want: BackhaulWireless,
		},
		{
			// A down interface returns EINVAL on carrier; operstate is the fallback.
			name: "carrier errors, operstate up => ethernet",
			fs: fakeFS{
				carrier:   {err: errors.New("invalid argument")},
				operstate: {data: []byte("up\n")},
			},
			want: BackhaulEthernet,
		},
		{
			name: "carrier errors, operstate down => wireless",
			fs: fakeFS{
				carrier:   {err: errors.New("invalid argument")},
				operstate: {data: []byte("down\n")},
			},
			want: BackhaulWireless,
		},
		{
			name: "interface absent => unknown",
			fs:   fakeFS{},
			want: BackhaulUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := SysfsBackhaul{Iface: iface, Read: tt.fs.read}
			if got := s.Backhaul(context.Background()); got != tt.want {
				t.Fatalf("Backhaul() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSysfsBackhaulNoIfaceIsUnknown(t *testing.T) {
	s := SysfsBackhaul{Iface: ""}
	if got := s.Backhaul(context.Background()); got != BackhaulUnknown {
		t.Fatalf("Backhaul() with no iface = %q, want %q", got, BackhaulUnknown)
	}
}
