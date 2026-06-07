// Package deviceled drives a node's status LED over the kernel sysfs LED class
// (/sys/class/leds/<name>/{brightness,trigger}). It exists so an installer can
// read a node's onboarding state from the device itself, without a companion
// app: blinking while unclaimed, a heartbeat while it joins, solid once active.
//
// Consistent with OMM's charter it drives the kernel's own LED interface rather
// than reimplementing anything, and it degrades gracefully: a board that lacks
// the configured LED simply does nothing (writes are skipped, no error), so the
// same daemon runs unchanged across hardware with different LED naming.
package deviceled

import (
	"os"
	"strconv"
)

// LEDController sets a named LED's brightness and kernel trigger.
type LEDController interface {
	SetBrightness(name string, value int) error
	SetTrigger(name, trigger string) error
}

// Sysfs is an LEDController backed by /sys/class/leds. Write and Stat are
// injected so the controller is testable without a real sysfs; both default to
// the os equivalents.
type Sysfs struct {
	Base  string                                                 // LED class dir; default "/sys/class/leds"
	Write func(path string, data []byte, perm os.FileMode) error // nil => os.WriteFile
	Stat  func(path string) (os.FileInfo, error)                 // nil => os.Stat
}

// New returns a Sysfs controller rooted at base (or the default when empty).
func New(base string) Sysfs {
	return Sysfs{Base: base}
}

func (s Sysfs) base() string {
	if s.Base == "" {
		return "/sys/class/leds"
	}
	return s.Base
}

// present reports whether the named LED directory exists, so writes to a board
// that lacks it are skipped rather than erroring.
func (s Sysfs) present(name string) bool {
	stat := s.Stat
	if stat == nil {
		stat = os.Stat
	}
	_, err := stat(s.base() + "/" + name)
	return err == nil
}

func (s Sysfs) writeFile(name, attr, value string) error {
	if !s.present(name) {
		return nil // graceful no-op on hardware without this LED
	}
	write := s.Write
	if write == nil {
		write = os.WriteFile
	}
	return write(s.base()+"/"+name+"/"+attr, []byte(value), 0o644)
}

// SetBrightness writes the LED's brightness value.
func (s Sysfs) SetBrightness(name string, value int) error {
	return s.writeFile(name, "brightness", strconv.Itoa(value))
}

// SetTrigger selects the LED's kernel trigger (e.g. "none", "timer", "heartbeat").
func (s Sysfs) SetTrigger(name, trigger string) error {
	return s.writeFile(name, "trigger", trigger)
}
