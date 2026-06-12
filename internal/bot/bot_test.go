package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eliau2005/statixagent/internal/alert"
	"github.com/eliau2005/statixagent/internal/collect"
	"github.com/eliau2005/statixagent/internal/procfs"
	"github.com/eliau2005/statixagent/internal/services"
	"github.com/eliau2005/statixagent/internal/sshwatch"
	"github.com/eliau2005/statixagent/internal/sysfs"
	"github.com/eliau2005/statixagent/internal/telegram"
)

func TestRouterAllowlistAndDispatch(t *testing.T) {
	r := NewRouter(42)
	r.Handle("status", func(ctx context.Context, args []string) string { return "STATUS" })
	r.Handle("ssh", func(ctx context.Context, args []string) string {
		return "SSH:" + strings.Join(args, ",")
	})

	if _, ok := r.Dispatch(context.Background(), telegram.Update{ChatID: 999, Text: "/status"}); ok {
		t.Fatal("foreign chat must be dropped silently")
	}
	if _, ok := r.Dispatch(context.Background(), telegram.Update{ChatID: 42, Text: "hello"}); ok {
		t.Fatal("non-command text must be ignored")
	}
	reply, ok := r.Dispatch(context.Background(), telegram.Update{ChatID: 42, Text: "/status"})
	if !ok || reply != "STATUS" {
		t.Fatalf("dispatch = %q, %v", reply, ok)
	}
	reply, _ = r.Dispatch(context.Background(), telegram.Update{ChatID: 42, Text: "/ssh history extra"})
	if reply != "SSH:history,extra" {
		t.Fatalf("args = %q", reply)
	}
	reply, _ = r.Dispatch(context.Background(), telegram.Update{ChatID: 42, Text: "/STATUS@mybot"})
	if reply != "STATUS" {
		t.Fatalf("case/@bot form = %q", reply)
	}
	reply, ok = r.Dispatch(context.Background(), telegram.Update{ChatID: 42, Text: "/nope"})
	if !ok || !strings.Contains(reply, "Unknown command") {
		t.Fatalf("unknown = %q, %v", reply, ok)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := Bytes(1536); got != "1.5 KiB" {
		t.Errorf("Bytes = %q", got)
	}
	if got := Bytes(8024904 * 1024); got != "7.7 GiB" {
		t.Errorf("Bytes = %q", got)
	}
	if got := Dur(26*time.Hour + 10*time.Minute); got != "1d 2h" {
		t.Errorf("Dur = %q", got)
	}
	if got := Dur(95 * time.Second); got != "1m 35s" {
		t.Errorf("Dur = %q", got)
	}
	if got := bar(73); got != "▰▰▰▰▰▰▰▱▱▱" {
		t.Errorf("bar = %q", got)
	}
}

func sampleSnapshot() collect.Snapshot {
	return collect.Snapshot{
		CPUTotal: collect.CPUUsage{Name: "cpu", Percent: 42.5},
		PerCore: []collect.CPUUsage{
			{Name: "cpu0", Percent: 80},
			{Name: "cpu1", Percent: 5},
		},
		Load: procfs.LoadAvg{Load1: 0.52, Load5: 0.58, Load15: 0.59},
		Mem: procfs.MemInfo{
			Total: 8 << 30, Available: 4 << 30, Free: 1 << 30,
			Buffers: 1 << 29, Cached: 1 << 30, SwapTotal: 2 << 30, SwapFree: 2 << 30,
		},
		Net: []collect.NetRate{{
			Name: "eth0", RxBytesPerSec: 1 << 20, TxBytesPerSec: 1 << 18,
			RxTotal: 5 << 30, TxTotal: 1 << 30,
		}},
		Mounts: []collect.MountUsage{{
			MountPoint: "/", TotalBytes: 40 << 30, FreeBytes: 10 << 30, UsedPercent: 75,
		}},
		Uptime:  73 * time.Hour,
		NumProc: 142,
		FileNR:  procfs.FileNR{Allocated: 2080, Max: 9000},
	}
}

func TestStatusFormat(t *testing.T) {
	th := sysfs.Thermal{Sensors: []sysfs.TempSensor{{Label: "pkg", Celsius: 61}}, Throttled: true}
	pw := sysfs.Power{HasAC: true, ACOnline: false, HasBattery: true,
		Batteries: []sysfs.Battery{{Name: "BAT0", Percent: 73, Status: "Discharging", TimeToEmpty: 4 * time.Hour}}}
	out := Status("myvps", sampleSnapshot(), th, pw)
	for _, want := range []string{
		"<b>myvps</b>", "up 3d 1h", "CPU 42%", "load 0.52",
		"RAM 50% of 8.0 GiB", "/ 75% used", "eth0 ↓1.0 MiB/s",
		"61°C max", "⚠️ throttling", "🔋 73% (Discharging)", "~4h 0m left",
		"142 processes", "2080 open fds",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Status missing %q in:\n%s", want, out)
		}
	}
}

func TestSectionFormats(t *testing.T) {
	s := sampleSnapshot()
	if out := CPU(s); !strings.Contains(out, "cpu0") || !strings.Contains(out, "▰") {
		t.Errorf("CPU:\n%s", out)
	}
	if out := Mem(s); !strings.Contains(out, "available 4.0 GiB") {
		t.Errorf("Mem:\n%s", out)
	}
	if out := Disk(s); !strings.Contains(out, "10.0 GiB free of 40.0 GiB") {
		t.Errorf("Disk:\n%s", out)
	}
	if out := Net(s); !strings.Contains(out, "total ↓5.0 GiB") {
		t.Errorf("Net:\n%s", out)
	}
	if out := Temp(sysfs.Thermal{}); !strings.Contains(out, "VPS") {
		t.Errorf("Temp empty:\n%s", out)
	}
	if out := Battery(sysfs.Power{}); !strings.Contains(out, "VPS") {
		t.Errorf("Battery empty:\n%s", out)
	}
}

func TestServiceAndSSHFormats(t *testing.T) {
	out := ServiceResults([]services.Result{
		{Name: "nginx.service", State: services.StateOK, Detail: "active/running"},
		{Name: "db.service", State: services.StateDown, Detail: "failed/failed"},
	})
	if !strings.Contains(out, "✅ nginx.service") || !strings.Contains(out, "❌ db.service") {
		t.Errorf("services:\n%s", out)
	}

	now := time.Date(2026, 6, 12, 18, 0, 0, 0, time.UTC)
	sess := Sessions(
		[]sshwatch.Session{{User: "alice", TTY: "pts/0", Host: "203.0.113.7", Since: now.Add(-30 * time.Minute)}},
		map[string]sshwatch.GeoInfo{"203.0.113.7": {Country: "Germany", City: "Berlin"}},
		now,
	)
	for _, want := range []string{"<b>alice</b>", "pts/0", "30m 0s", "Berlin, Germany"} {
		if !strings.Contains(sess, want) {
			t.Errorf("sessions missing %q:\n%s", want, sess)
		}
	}

	ev := SSHEvents("Failed attempts", []sshwatch.Event{
		{Kind: sshwatch.EventFailed, User: "root", IP: "192.0.2.4", Method: "password", At: now},
		{Kind: sshwatch.EventInvalidUser, User: "admin<script>", IP: "192.0.2.4", InvalidUser: true, At: now},
	})
	if !strings.Contains(ev, "❌") || !strings.Contains(ev, "invalid user") {
		t.Errorf("events:\n%s", ev)
	}
	if strings.Contains(ev, "<script>") {
		t.Error("usernames must be HTML-escaped")
	}
}

func TestAlertMsg(t *testing.T) {
	a := alert.Alert{Key: "cpu", Severity: alert.Critical, Title: "CPU usage", Body: "97.0% is above the 90.0% threshold"}
	out := AlertMsg("myvps", a)
	if !strings.Contains(out, "🚨") || !strings.Contains(out, "myvps") {
		t.Errorf("alert:\n%s", out)
	}
	rec := alert.Alert{Key: "cpu", Title: "CPU usage", Body: "recovered: now 50.0%", Resolved: true}
	if out := AlertMsg("myvps", rec); !strings.Contains(out, "✅") {
		t.Errorf("resolved alert:\n%s", out)
	}
}
