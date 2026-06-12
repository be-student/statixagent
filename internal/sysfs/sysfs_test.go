package sysfs

import (
	"testing"
	"testing/fstest"
	"time"
)

func f(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content + "\n")}
}

func TestReadThermal(t *testing.T) {
	sys := fstest.MapFS{
		"class/thermal/thermal_zone0/temp":        f("47000"),
		"class/thermal/thermal_zone0/type":        f("x86_pkg_temp"),
		"class/thermal/thermal_zone1/temp":        f("bogus"), // unreadable → skipped
		"class/thermal/cooling_device0/type":      f("Processor"),
		"class/thermal/cooling_device0/cur_state": f("2"),
		"class/thermal/cooling_device1/type":      f("Fan"),
		"class/thermal/cooling_device1/cur_state": f("3"), // non-processor → ignored
		"class/hwmon/hwmon0/name":                 f("coretemp"),
		"class/hwmon/hwmon0/temp1_input":          f("52000"),
		"class/hwmon/hwmon0/temp1_label":          f("Core 0"),
		"class/hwmon/hwmon1/name":                 f("thinkpad"),
		"class/hwmon/hwmon1/fan1_input":           f("3120"),
	}
	th, err := ReadThermal(sys)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Sensors) != 2 {
		t.Fatalf("sensors = %+v", th.Sensors)
	}
	if th.Sensors[0].Label != "x86_pkg_temp" || th.Sensors[0].Celsius != 47 {
		t.Errorf("zone sensor = %+v", th.Sensors[0])
	}
	if th.Sensors[1].Label != "Core 0" || th.Sensors[1].Celsius != 52 {
		t.Errorf("hwmon sensor = %+v", th.Sensors[1])
	}
	if got := th.MaxCelsius(); got != 52 {
		t.Errorf("max = %.1f", got)
	}
	if len(th.Fans) != 1 || th.Fans[0].RPM != 3120 {
		t.Errorf("fans = %+v", th.Fans)
	}
	if !th.Throttled {
		t.Error("processor cooling device at cur_state 2 must report throttled")
	}
}

func TestReadThermalEmptySys(t *testing.T) {
	th, err := ReadThermal(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Sensors) != 0 || th.Throttled || th.MaxCelsius() != 0 {
		t.Errorf("VPS with no sensors must be empty: %+v", th)
	}
}

func TestReadPowerLaptopDischarging(t *testing.T) {
	sys := fstest.MapFS{
		"class/power_supply/AC/type":                 f("Mains"),
		"class/power_supply/AC/online":               f("0"),
		"class/power_supply/BAT0/type":               f("Battery"),
		"class/power_supply/BAT0/capacity":           f("73"),
		"class/power_supply/BAT0/status":             f("Discharging"),
		"class/power_supply/BAT0/energy_full":        f("48000000"),
		"class/power_supply/BAT0/energy_full_design": f("57000000"),
		"class/power_supply/BAT0/energy_now":         f("35040000"),
		"class/power_supply/BAT0/power_now":          f("8760000"),
	}
	p, err := ReadPower(sys)
	if err != nil {
		t.Fatal(err)
	}
	if !p.OnBattery() {
		t.Error("AC offline + battery present must be OnBattery")
	}
	b := p.Batteries[0]
	if b.Percent != 73 || b.Status != "Discharging" {
		t.Errorf("battery = %+v", b)
	}
	if b.HealthPercent < 84.1 || b.HealthPercent > 84.3 { // 48/57
		t.Errorf("health = %.2f", b.HealthPercent)
	}
	if b.Watts < 8.75 || b.Watts > 8.77 {
		t.Errorf("watts = %.3f", b.Watts)
	}
	if want := 4 * time.Hour; b.TimeToEmpty != want { // 35.04 Wh / 8.76 W
		t.Errorf("time to empty = %s, want %s", b.TimeToEmpty, want)
	}
}

func TestReadPowerChargeFallback(t *testing.T) {
	sys := fstest.MapFS{
		"class/power_supply/BAT0/type":               f("Battery"),
		"class/power_supply/BAT0/capacity":           f("100"),
		"class/power_supply/BAT0/status":             f("Full"),
		"class/power_supply/BAT0/charge_full":        f("4000000"),
		"class/power_supply/BAT0/charge_full_design": f("5000000"),
	}
	p, err := ReadPower(sys)
	if err != nil {
		t.Fatal(err)
	}
	if p.Batteries[0].HealthPercent != 80 {
		t.Errorf("health = %.2f, want 80", p.Batteries[0].HealthPercent)
	}
	if p.Batteries[0].TimeToEmpty != 0 {
		t.Errorf("full battery must have no time-to-empty")
	}
}

func TestReadPowerVPS(t *testing.T) {
	p, err := ReadPower(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if p.OnBattery() || p.HasAC || p.HasBattery {
		t.Errorf("VPS must report no power hardware: %+v", p)
	}
}
