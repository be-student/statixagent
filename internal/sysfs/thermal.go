// Package sysfs reads thermal and power state from the /sys filesystem.
// All functions take an fs.FS rooted at /sys so tests can substitute a
// fstest.MapFS; the linux sampler passes os.DirFS("/sys").
package sysfs

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// TempSensor is one thermal reading.
type TempSensor struct {
	Label   string // zone type ("x86_pkg_temp") or hwmon label ("Core 0")
	Celsius float64
}

// Fan is one fan speed reading.
type Fan struct {
	Label string
	RPM   int
}

// Thermal is the combined thermal state.
type Thermal struct {
	Sensors   []TempSensor
	Fans      []Fan
	Throttled bool // any cooling device actively throttling
}

// MaxCelsius returns the hottest sensor, or 0 if there are none.
func (t Thermal) MaxCelsius() float64 {
	var max float64
	for _, s := range t.Sensors {
		if s.Celsius > max {
			max = s.Celsius
		}
	}
	return max
}

// ReadThermal collects temperatures from thermal zones and hwmon, fan speeds
// from hwmon, and throttle state from cooling devices. Missing directories
// are not errors — VPS guests often expose nothing here.
func ReadThermal(sysfs fs.FS) (Thermal, error) {
	var t Thermal

	zones, _ := fs.Glob(sysfs, "class/thermal/thermal_zone*")
	for _, z := range zones {
		mc, err := readInt(sysfs, z+"/temp")
		if err != nil {
			continue // zone can vanish or be unreadable; skip it
		}
		label, _ := readString(sysfs, z+"/type")
		if label == "" {
			label = strings.TrimPrefix(z, "class/thermal/")
		}
		t.Sensors = append(t.Sensors, TempSensor{Label: label, Celsius: float64(mc) / 1000})
	}

	hwmons, _ := fs.Glob(sysfs, "class/hwmon/hwmon*")
	for _, h := range hwmons {
		name, _ := readString(sysfs, h+"/name")
		temps, _ := fs.Glob(sysfs, h+"/temp*_input")
		for _, ti := range temps {
			mc, err := readInt(sysfs, ti)
			if err != nil {
				continue
			}
			label, _ := readString(sysfs, strings.TrimSuffix(ti, "_input")+"_label")
			if label == "" {
				label = name + "/" + strings.TrimSuffix(ti[len(h)+1:], "_input")
			}
			t.Sensors = append(t.Sensors, TempSensor{Label: label, Celsius: float64(mc) / 1000})
		}
		fans, _ := fs.Glob(sysfs, h+"/fan*_input")
		for _, fi := range fans {
			rpm, err := readInt(sysfs, fi)
			if err != nil {
				continue
			}
			label, _ := readString(sysfs, strings.TrimSuffix(fi, "_input")+"_label")
			if label == "" {
				label = name + "/" + strings.TrimSuffix(fi[len(h)+1:], "_input")
			}
			t.Fans = append(t.Fans, Fan{Label: label, RPM: int(rpm)})
		}
	}

	// A processor cooling device with cur_state > 0 means active throttling.
	cdevs, _ := fs.Glob(sysfs, "class/thermal/cooling_device*")
	for _, cd := range cdevs {
		typ, _ := readString(sysfs, cd+"/type")
		if !strings.Contains(strings.ToLower(typ), "processor") &&
			!strings.Contains(strings.ToLower(typ), "intel_powerclamp") {
			continue
		}
		cur, err := readInt(sysfs, cd+"/cur_state")
		if err == nil && cur > 0 {
			t.Throttled = true
		}
	}
	return t, nil
}

func readString(sysfs fs.FS, path string) (string, error) {
	b, err := fs.ReadFile(sysfs, path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readInt(sysfs fs.FS, path string) (int64, error) {
	s, err := readString(sysfs, path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sysfs: %s: %w", path, err)
	}
	return v, nil
}
