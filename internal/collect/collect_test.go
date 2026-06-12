package collect

import (
	"testing"
	"time"

	"github.com/eliau2005/statixagent/internal/procfs"
)

func mkSample(at time.Time, busy, total uint64, rx, tx uint64) Sample {
	idle := total - busy
	return Sample{
		At: at,
		Stat: procfs.Stat{
			Aggregate: procfs.CPUStat{Name: "cpu", User: busy, Idle: idle},
			PerCore: []procfs.CPUStat{
				{Name: "cpu0", User: busy / 2, Idle: idle / 2},
				{Name: "cpu1", User: busy / 2, Idle: idle / 2},
			},
		},
		Net: []procfs.NetDev{{Name: "eth0", RxBytes: rx, TxBytes: tx}},
		Disks: []procfs.DiskStat{{
			Name:           "sda",
			ReadsCompleted: total / 100, SectorsRead: rx / 512,
			WritesComplete: total / 200, SectorsWritten: tx / 512,
		}},
	}
}

func TestComputeRates(t *testing.T) {
	t0 := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	// over 10s: +500 busy of +1000 total ticks = 50% CPU; +10 MB rx = 1 MB/s
	prev := mkSample(t0, 1000, 10000, 100_000_000, 50_000_000)
	cur := mkSample(t0.Add(10*time.Second), 1500, 11000, 110_485_760, 55_242_880)

	s := Compute(prev, cur)

	if got := s.CPUTotal.Percent; got < 49.9 || got > 50.1 {
		t.Errorf("cpu%% = %.2f, want 50", got)
	}
	if len(s.PerCore) != 2 {
		t.Fatalf("per-core = %d", len(s.PerCore))
	}
	if len(s.Net) != 1 {
		t.Fatalf("net = %d", len(s.Net))
	}
	rx := s.Net[0].RxBytesPerSec
	if rx < 1_048_575 || rx > 1_048_577 {
		t.Errorf("rx = %.0f B/s, want ~1048576", rx)
	}
	if s.Net[0].RxTotal != 110_485_760 {
		t.Errorf("rx total = %d", s.Net[0].RxTotal)
	}
	if s.Disks[0].ReadBytesPerSec <= 0 || s.Disks[0].IOPS <= 0 {
		t.Errorf("disk rates = %+v", s.Disks[0])
	}
}

func TestComputeFirstSampleHasZeroRates(t *testing.T) {
	cur := mkSample(time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC), 5000, 10000, 1e9, 1e9)
	s := Compute(Sample{}, cur)
	if s.CPUTotal.Percent != 0 {
		t.Errorf("first-tick cpu%% = %.2f, want 0", s.CPUTotal.Percent)
	}
	if s.Net[0].RxBytesPerSec != 0 {
		t.Errorf("first-tick rx = %.0f, want 0", s.Net[0].RxBytesPerSec)
	}
	if s.Net[0].RxTotal != 1e9 {
		t.Errorf("totals must still pass through, got %d", s.Net[0].RxTotal)
	}
}

func TestComputeCounterReset(t *testing.T) {
	t0 := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	prev := mkSample(t0, 1000, 10000, 5_000_000, 5_000_000)
	cur := mkSample(t0.Add(10*time.Second), 50, 500, 1000, 1000) // counters went backward
	s := Compute(prev, cur)
	if s.CPUTotal.Percent != 0 || s.Net[0].RxBytesPerSec != 0 {
		t.Errorf("reset must yield zero rates: cpu=%.2f rx=%.2f", s.CPUTotal.Percent, s.Net[0].RxBytesPerSec)
	}
}

func TestIsPartition(t *testing.T) {
	all := []string{"sda", "sda1", "sda2", "nvme0n1", "nvme0n1p1", "mmcblk0", "mmcblk0p2", "sdb"}
	cases := map[string]bool{
		"sda":       false,
		"sda1":      true,
		"sda2":      true,
		"nvme0n1":   false,
		"nvme0n1p1": true,
		"mmcblk0":   false,
		"mmcblk0p2": true,
		"sdb":       false,
	}
	for name, want := range cases {
		if got := IsPartition(name, all); got != want {
			t.Errorf("IsPartition(%q) = %v, want %v", name, got, want)
		}
	}
}
