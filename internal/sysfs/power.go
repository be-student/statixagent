package sysfs

import (
	"io/fs"
	"strings"
	"time"
)

// Battery is the state of one battery from /sys/class/power_supply.
type Battery struct {
	Name          string
	Percent       float64
	Status        string  // Charging, Discharging, Full, Not charging
	HealthPercent float64 // full capacity vs design capacity; 0 if unknown
	Watts         float64 // current draw/charge rate; 0 if unknown
	TimeToEmpty   time.Duration
}

// Power is the machine's power state.
type Power struct {
	ACOnline   bool
	HasAC      bool // an AC adapter device exists at all
	HasBattery bool
	Batteries  []Battery
}

// OnBattery reports whether the machine is running unplugged. Machines
// without any AC adapter device (VPS) are never "on battery".
func (p Power) OnBattery() bool {
	return p.HasAC && !p.ACOnline && p.HasBattery
}

// ReadPower reads AC and battery state. Missing power_supply entries are
// normal on servers and yield an empty Power, not an error.
func ReadPower(sysfs fs.FS) (Power, error) {
	var p Power
	supplies, _ := fs.Glob(sysfs, "class/power_supply/*")
	for _, s := range supplies {
		typ, _ := readString(sysfs, s+"/type")
		switch typ {
		case "Mains", "USB", "ADP", "AC":
			p.HasAC = true
			if online, err := readInt(sysfs, s+"/online"); err == nil && online == 1 {
				p.ACOnline = true
			}
		case "Battery":
			b := readBattery(sysfs, s)
			p.HasBattery = true
			p.Batteries = append(p.Batteries, b)
		}
	}
	return p, nil
}

func readBattery(sysfs fs.FS, dir string) Battery {
	b := Battery{Name: dir[strings.LastIndexByte(dir, '/')+1:]}
	if v, err := readInt(sysfs, dir+"/capacity"); err == nil {
		b.Percent = float64(v)
	}
	b.Status, _ = readString(sysfs, dir+"/status")

	// Energy-reporting batteries expose µWh / µW; charge-reporting ones
	// expose µAh / µA with voltage. Prefer energy when present.
	full, errF := readInt(sysfs, dir+"/energy_full")
	design, errD := readInt(sysfs, dir+"/energy_full_design")
	now, errN := readInt(sysfs, dir+"/energy_now")
	rate, errR := readInt(sysfs, dir+"/power_now")
	if errF != nil { // fall back to charge_* (µAh); ratios cancel units
		full, errF = readInt(sysfs, dir+"/charge_full")
		design, errD = readInt(sysfs, dir+"/charge_full_design")
		now, errN = readInt(sysfs, dir+"/charge_now")
		rate, errR = readInt(sysfs, dir+"/current_now")
	}
	if errF == nil && errD == nil && design > 0 {
		b.HealthPercent = 100 * float64(full) / float64(design)
	}
	if errR == nil {
		b.Watts = float64(rate) / 1e6 // exact for µW; approximate for µA paths
		if errN == nil && rate > 0 && strings.EqualFold(b.Status, "Discharging") {
			hours := float64(now) / float64(rate)
			b.TimeToEmpty = time.Duration(hours * float64(time.Hour))
		}
	}
	return b
}
