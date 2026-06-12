// Package collect defines the sampling model: a raw Sample of cumulative
// counters and gauges, and a Snapshot of human-meaningful values (percentages
// and rates) computed from two consecutive samples. The math is pure so it
// tests anywhere; only the linux Sampler reads the real /proc.
package collect

import (
	"time"

	"github.com/eliau2005/statixagent/internal/procfs"
)

// Sample is one raw reading. Counter fields are cumulative since boot;
// rate computation needs two samples.
type Sample struct {
	At time.Time

	Stat    procfs.Stat
	Mem     procfs.MemInfo
	Load    procfs.LoadAvg
	Uptime  time.Duration
	Net     []procfs.NetDev
	Disks   []procfs.DiskStat
	FileNR  procfs.FileNR
	NumProc int

	Mounts []MountUsage // gauge, filled by the linux sampler via statfs
}

// MountUsage is disk space for one mounted filesystem.
type MountUsage struct {
	MountPoint  string
	TotalBytes  uint64
	FreeBytes   uint64 // free for unprivileged users
	UsedPercent float64
}

// CPUUsage is the busy percentage for one CPU over the sample window.
type CPUUsage struct {
	Name    string
	Percent float64
}

// NetRate is the transfer rate of one interface plus its lifetime totals.
type NetRate struct {
	Name          string
	RxBytesPerSec float64
	TxBytesPerSec float64
	RxTotal       uint64
	TxTotal       uint64
}

// DiskRate is the I/O rate of one block device.
type DiskRate struct {
	Name             string
	ReadBytesPerSec  float64
	WriteBytesPerSec float64
	IOPS             float64
}

// sectorSize is the fixed unit of /proc/diskstats sector counters,
// independent of the device's real sector size.
const sectorSize = 512

// Snapshot is the computed state of the machine at a moment, ready for
// formatting and threshold evaluation.
type Snapshot struct {
	At time.Time

	CPUTotal CPUUsage
	PerCore  []CPUUsage
	Load     procfs.LoadAvg

	Mem procfs.MemInfo

	Net   []NetRate
	Disks []DiskRate

	Mounts  []MountUsage
	Uptime  time.Duration
	NumProc int
	FileNR  procfs.FileNR
}

// Compute derives a Snapshot from two consecutive samples. prev may be the
// zero Sample on the first tick; rates are then reported as zero rather
// than as nonsense derived from since-boot counters.
func Compute(prev, cur Sample) Snapshot {
	s := Snapshot{
		At:      cur.At,
		Load:    cur.Load,
		Mem:     cur.Mem,
		Mounts:  cur.Mounts,
		Uptime:  cur.Uptime,
		NumProc: cur.NumProc,
		FileNR:  cur.FileNR,
	}
	first := prev.At.IsZero()
	elapsed := cur.At.Sub(prev.At).Seconds()

	s.CPUTotal = CPUUsage{Name: "cpu", Percent: cpuPercent(prev.Stat.Aggregate, cur.Stat.Aggregate, first)}
	prevCores := map[string]procfs.CPUStat{}
	for _, c := range prev.Stat.PerCore {
		prevCores[c.Name] = c
	}
	for _, c := range cur.Stat.PerCore {
		s.PerCore = append(s.PerCore, CPUUsage{Name: c.Name, Percent: cpuPercent(prevCores[c.Name], c, first)})
	}

	prevNet := map[string]procfs.NetDev{}
	for _, n := range prev.Net {
		prevNet[n.Name] = n
	}
	for _, n := range cur.Net {
		r := NetRate{Name: n.Name, RxTotal: n.RxBytes, TxTotal: n.TxBytes}
		if p, ok := prevNet[n.Name]; ok && !first && elapsed > 0 {
			r.RxBytesPerSec = counterRate(p.RxBytes, n.RxBytes, elapsed)
			r.TxBytesPerSec = counterRate(p.TxBytes, n.TxBytes, elapsed)
		}
		s.Net = append(s.Net, r)
	}

	prevDisk := map[string]procfs.DiskStat{}
	for _, d := range prev.Disks {
		prevDisk[d.Name] = d
	}
	for _, d := range cur.Disks {
		r := DiskRate{Name: d.Name}
		if p, ok := prevDisk[d.Name]; ok && !first && elapsed > 0 {
			r.ReadBytesPerSec = counterRate(p.SectorsRead, d.SectorsRead, elapsed) * sectorSize
			r.WriteBytesPerSec = counterRate(p.SectorsWritten, d.SectorsWritten, elapsed) * sectorSize
			ios := counterRate(p.ReadsCompleted, d.ReadsCompleted, elapsed) +
				counterRate(p.WritesComplete, d.WritesComplete, elapsed)
			r.IOPS = ios
		}
		s.Disks = append(s.Disks, r)
	}
	return s
}

// cpuPercent returns busy time as a percentage of total ticks elapsed
// between two readings of the same CPU.
func cpuPercent(prev, cur procfs.CPUStat, first bool) float64 {
	if first {
		return 0
	}
	dTotal := int64(cur.Total()) - int64(prev.Total())
	dBusy := int64(cur.Busy()) - int64(prev.Busy())
	if dTotal <= 0 || dBusy < 0 {
		return 0 // counter reset or no time passed
	}
	return 100 * float64(dBusy) / float64(dTotal)
}

// counterRate converts a cumulative counter delta to a per-second rate,
// treating wraparound/reset as zero.
func counterRate(prev, cur uint64, elapsedSec float64) float64 {
	if cur < prev || elapsedSec <= 0 {
		return 0
	}
	return float64(cur-prev) / elapsedSec
}

// IsPartition reports whether a /proc/diskstats device name looks like a
// partition of another listed device (sda1, nvme0n1p2, mmcblk0p1) rather
// than a whole disk. Used to keep rate reporting per physical device.
func IsPartition(name string, allNames []string) bool {
	for _, other := range allNames {
		if other == name || len(other) >= len(name) {
			continue
		}
		rest := name[len(other):]
		if !hasPrefixDevice(name, other) {
			continue
		}
		// rest must be digits ("1") or p+digits ("p3")
		if rest[0] == 'p' {
			rest = rest[1:]
		}
		if rest != "" && allDigits(rest) {
			return true
		}
	}
	return false
}

func hasPrefixDevice(name, prefix string) bool {
	return len(name) > len(prefix) && name[:len(prefix)] == prefix
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
