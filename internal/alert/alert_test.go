package alert

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 6, 12, 17, 0, 0, 0, time.UTC)

func cpuOpts(v float64) ThresholdOpts {
	return ThresholdOpts{
		Key: "cpu", Title: "CPU usage", Severity: Warning,
		Value: v, Threshold: 90, ClearMargin: 5, Unit: "%",
	}
}

func TestThresholdFireOnceAndRecover(t *testing.T) {
	e := New(10 * time.Minute)

	if a := e.Threshold(cpuOpts(50), t0); a != nil {
		t.Fatalf("below threshold fired: %+v", a)
	}
	a := e.Threshold(cpuOpts(95), t0.Add(time.Minute))
	if a == nil || a.Resolved || a.Severity != Warning {
		t.Fatalf("crossing must fire: %+v", a)
	}
	if !strings.Contains(a.Body, "95.0% is above the 90.0%") {
		t.Errorf("body = %q", a.Body)
	}
	if a := e.Threshold(cpuOpts(97), t0.Add(2*time.Minute)); a != nil {
		t.Fatalf("still violating must not re-fire: %+v", a)
	}
	// 88% is under threshold but inside the 5-point clear margin: no recovery yet.
	if a := e.Threshold(cpuOpts(88), t0.Add(3*time.Minute)); a != nil {
		t.Fatalf("inside hysteresis margin must stay silent: %+v", a)
	}
	if !e.Active("cpu") {
		t.Fatal("must still be active inside margin")
	}
	rec := e.Threshold(cpuOpts(60), t0.Add(4*time.Minute))
	if rec == nil || !rec.Resolved {
		t.Fatalf("recovery alert expected: %+v", rec)
	}
	if e.Active("cpu") {
		t.Fatal("must be inactive after recovery")
	}
}

func TestThresholdCooldownBlocksFlapping(t *testing.T) {
	e := New(10 * time.Minute)
	e.Threshold(cpuOpts(95), t0)                  // fire
	e.Threshold(cpuOpts(50), t0.Add(time.Minute)) // recover
	if a := e.Threshold(cpuOpts(95), t0.Add(2*time.Minute)); a != nil {
		t.Fatalf("re-fire inside cooldown: %+v", a)
	}
	if a := e.Threshold(cpuOpts(95), t0.Add(11*time.Minute)); a == nil {
		t.Fatal("re-fire after cooldown must work")
	}
}

func TestThresholdBelowMode(t *testing.T) {
	e := New(0)
	battery := func(v float64) ThresholdOpts {
		return ThresholdOpts{
			Key: "battery", Title: "Battery", Severity: Critical,
			Value: v, Threshold: 15, ClearMargin: 5, Below: true, Unit: "%",
		}
	}
	if a := e.Threshold(battery(80), t0); a != nil {
		t.Fatalf("healthy battery fired: %+v", a)
	}
	a := e.Threshold(battery(12), t0.Add(time.Minute))
	if a == nil || !strings.Contains(a.Body, "below") {
		t.Fatalf("low battery must fire with 'below': %+v", a)
	}
	if a := e.Threshold(battery(17), t0.Add(2*time.Minute)); a != nil {
		t.Fatalf("17%% is inside clear margin (15+5): %+v", a)
	}
	rec := e.Threshold(battery(45), t0.Add(3*time.Minute))
	if rec == nil || !rec.Resolved {
		t.Fatalf("charging past margin must resolve: %+v", rec)
	}
}

func TestEventCooldown(t *testing.T) {
	e := New(time.Hour)
	a := e.Event("power-loss", "Power lost", "running on battery", Critical, t0, 0)
	if a == nil || a.Severity != Critical {
		t.Fatalf("first event must emit: %+v", a)
	}
	if a := e.Event("power-loss", "Power lost", "again", Critical, t0.Add(time.Minute), 0); a != nil {
		t.Fatalf("inside default cooldown: %+v", a)
	}
	// Override: ssh logins always alert (cooldown -1 → effectively zero? use small override)
	if a := e.Event("ssh-login", "login", "alice", Info, t0, time.Nanosecond); a == nil {
		t.Fatal("first ssh login must emit")
	}
	if a := e.Event("ssh-login", "login", "alice again", Info, t0.Add(time.Second), time.Nanosecond); a == nil {
		t.Fatal("tiny override cooldown must allow immediate re-emit")
	}
}
