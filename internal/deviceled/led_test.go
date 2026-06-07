package deviceled

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"
)

// recordingFS captures sysfs writes in order and answers Stat from a set of
// present LED names, so tests assert exactly what the controller writes without
// touching a real /sys/class/leds.
type recordingFS struct {
	writes  []write
	present map[string]bool // LED directory names that "exist"
}

type write struct {
	path string
	data string
}

func newRecordingFS(present ...string) *recordingFS {
	p := map[string]bool{}
	for _, n := range present {
		p[n] = true
	}
	return &recordingFS{present: p}
}

func (r *recordingFS) write(path string, data []byte, _ os.FileMode) error {
	r.writes = append(r.writes, struct {
		path string
		data string
	}{path, string(data)})
	return nil
}

func (r *recordingFS) stat(path string) (os.FileInfo, error) {
	// path is "<base>/<name>"; match on the trailing component.
	for name := range r.present {
		if path == "/sys/class/leds/"+name {
			return fakeInfo{}, nil
		}
	}
	return nil, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
}

type fakeInfo struct{}

func (fakeInfo) Name() string       { return "" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return true }
func (fakeInfo) Sys() any           { return nil }

func newSysfs(fsys *recordingFS) Sysfs {
	return Sysfs{Base: "/sys/class/leds", Write: fsys.write, Stat: fsys.stat}
}

func TestSysfsWritesBrightnessAndTrigger(t *testing.T) {
	fsys := newRecordingFS("status")
	led := newSysfs(fsys)

	if err := led.SetTrigger("status", "none"); err != nil {
		t.Fatalf("SetTrigger: %v", err)
	}
	if err := led.SetBrightness("status", 255); err != nil {
		t.Fatalf("SetBrightness: %v", err)
	}

	if len(fsys.writes) != 2 {
		t.Fatalf("expected 2 writes, got %d: %+v", len(fsys.writes), fsys.writes)
	}
	if fsys.writes[0].path != "/sys/class/leds/status/trigger" || fsys.writes[0].data != "none" {
		t.Fatalf("unexpected trigger write: %+v", fsys.writes[0])
	}
	if fsys.writes[1].path != "/sys/class/leds/status/brightness" || fsys.writes[1].data != "255" {
		t.Fatalf("unexpected brightness write: %+v", fsys.writes[1])
	}
}

func TestSysfsGracefulNoOpWhenLEDAbsent(t *testing.T) {
	fsys := newRecordingFS() // no LEDs present
	led := newSysfs(fsys)

	if err := led.SetTrigger("missing", "timer"); err != nil {
		t.Fatalf("expected nil error for absent LED, got %v", err)
	}
	if err := led.SetBrightness("missing", 255); err != nil {
		t.Fatalf("expected nil error for absent LED, got %v", err)
	}
	if len(fsys.writes) != 0 {
		t.Fatalf("expected no writes for absent LED, got %+v", fsys.writes)
	}
}

// recordingLED records the order of trigger/brightness calls for Apply tests.
type recordingLED struct {
	calls []string
	err   error
}

func (r *recordingLED) SetBrightness(name string, value int) error {
	r.calls = append(r.calls, "brightness")
	return r.err
}
func (r *recordingLED) SetTrigger(name, trigger string) error {
	r.calls = append(r.calls, "trigger:"+trigger)
	return r.err
}

func TestStateFor(t *testing.T) {
	tests := []struct {
		complete   bool
		activeHome string
		want       State
	}{
		{complete: false, activeHome: "", want: StateUnclaimed},
		{complete: false, activeHome: "home-1", want: StateUnclaimed},
		{complete: true, activeHome: "", want: StateEnrolling},
		{complete: true, activeHome: "home-1", want: StateActive},
	}
	for _, tt := range tests {
		if got := StateFor(tt.complete, tt.activeHome); got != tt.want {
			t.Fatalf("StateFor(%v,%q) = %q, want %q", tt.complete, tt.activeHome, got, tt.want)
		}
	}
}

func TestApplyWritesTriggerBeforeBrightness(t *testing.T) {
	led := &recordingLED{}
	if err := Apply(led, "status", PatternFor(StateActive)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Active is solid-on: trigger "none" first, then brightness.
	if len(led.calls) != 2 || led.calls[0] != "trigger:none" || led.calls[1] != "brightness" {
		t.Fatalf("expected trigger-before-brightness, got %+v", led.calls)
	}
}

func TestApplyTriggerOnlyForBlinkingStates(t *testing.T) {
	led := &recordingLED{}
	if err := Apply(led, "status", PatternFor(StateUnclaimed)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A kernel trigger owns brightness, so no brightness write follows.
	if len(led.calls) != 1 || led.calls[0] == "brightness" {
		t.Fatalf("expected a single trigger write, got %+v", led.calls)
	}
}

func TestReactorAppliesOnlyOnChange(t *testing.T) {
	led := &recordingLED{}
	r := NewReactor(led, "status")

	wrote, err := r.Update(StateUnclaimed)
	if err != nil || !wrote {
		t.Fatalf("first Update should write: wrote=%v err=%v", wrote, err)
	}
	// Same state again must not re-write.
	wrote, err = r.Update(StateUnclaimed)
	if err != nil || wrote {
		t.Fatalf("repeat Update should be a no-op: wrote=%v err=%v", wrote, err)
	}
	// A new state writes again.
	wrote, err = r.Update(StateActive)
	if err != nil || !wrote {
		t.Fatalf("changed Update should write: wrote=%v err=%v", wrote, err)
	}
}

func TestReactorPropagatesError(t *testing.T) {
	led := &recordingLED{err: errors.New("boom")}
	r := NewReactor(led, "status")
	if _, err := r.Update(StateActive); err == nil {
		t.Fatal("expected error from Update to propagate")
	}
}
