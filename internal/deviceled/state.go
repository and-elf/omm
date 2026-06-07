package deviceled

// State is the node's onboarding state as far as the status LED cares. It is
// derived from the daemon's persisted signals (setup-complete flag + active
// home), not the in-memory enrollment state machine.
type State string

const (
	// StateUnclaimed: setup has not completed — the node is waiting to be
	// onboarded. The LED blinks to say "set me up".
	StateUnclaimed State = "unclaimed"
	// StateEnrolling: setup completed but no home is active yet — the node is
	// joining/applying a profile. The LED pulses (heartbeat).
	StateEnrolling State = "enrolling"
	// StateActive: a home is active and its profile applied. The LED is solid.
	StateActive State = "active"
)

// StateFor derives the LED state from the two persisted onboarding signals:
// the setup-complete flag and the active home id.
func StateFor(setupComplete bool, activeHome string) State {
	switch {
	case !setupComplete:
		return StateUnclaimed
	case activeHome == "":
		return StateEnrolling
	default:
		return StateActive
	}
}

// Pattern is how a State is rendered on the LED: a kernel trigger, plus a
// brightness applied only when the trigger leaves brightness under our control.
type Pattern struct {
	Trigger    string // sysfs LED trigger: "none", "timer", "heartbeat"
	Brightness int    // applied only when Trigger == "none"
}

// PatternFor maps a State to its LED pattern.
func PatternFor(s State) Pattern {
	switch s {
	case StateActive:
		return Pattern{Trigger: "none", Brightness: 255} // solid on
	case StateEnrolling:
		return Pattern{Trigger: "heartbeat"} // pulsing while it joins
	default: // StateUnclaimed
		return Pattern{Trigger: "timer"} // blinking: needs setup
	}
}

// Apply sets the LED to a pattern. The trigger is written first; a kernel
// trigger (timer/heartbeat) then owns brightness, so brightness is written only
// for the "none" trigger (solid states).
func Apply(led LEDController, name string, p Pattern) error {
	if err := led.SetTrigger(name, p.Trigger); err != nil {
		return err
	}
	if p.Trigger == "none" {
		return led.SetBrightness(name, p.Brightness)
	}
	return nil
}

// Reactor applies an LED pattern only when the onboarding state changes, so the
// daemon can poll on a ticker without churning sysfs writes every tick.
type Reactor struct {
	led  LEDController
	name string
	last State
	set  bool
}

// NewReactor returns a reactor that drives the named LED.
func NewReactor(led LEDController, name string) *Reactor {
	return &Reactor{led: led, name: name}
}

// Update applies the pattern for s when s differs from the last applied state
// (or on the first call). It reports whether it wrote to the LED.
func (r *Reactor) Update(s State) (bool, error) {
	if r.set && s == r.last {
		return false, nil
	}
	if err := Apply(r.led, r.name, PatternFor(s)); err != nil {
		return false, err
	}
	r.last = s
	r.set = true
	return true, nil
}
