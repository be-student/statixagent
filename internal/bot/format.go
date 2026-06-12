package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/eliau2005/statixagent/internal/alert"
	"github.com/eliau2005/statixagent/internal/collect"
	"github.com/eliau2005/statixagent/internal/dockermon"
	"github.com/eliau2005/statixagent/internal/services"
	"github.com/eliau2005/statixagent/internal/sshwatch"
	"github.com/eliau2005/statixagent/internal/sysfs"
)

// Bytes renders a byte count with binary units.
func Bytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Rate renders bytes/second.
func Rate(bps float64) string {
	return Bytes(uint64(bps)) + "/s"
}

// Dur renders a duration compactly: "3d 4h", "2h 5m", "42s".
func Dur(d time.Duration) string {
	d = d.Round(time.Second)
	day := 24 * time.Hour
	switch {
	case d >= day:
		return fmt.Sprintf("%dd %dh", d/day, (d%day)/time.Hour)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", d/time.Hour, (d%time.Hour)/time.Minute)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", d/time.Minute, (d%time.Minute)/time.Second)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

func esc(s string) string { return html.EscapeString(s) }

// Status renders the /status overview.
func Status(hostname string, s collect.Snapshot, th sysfs.Thermal, pw sysfs.Power) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b> — up %s\n\n", esc(hostname), Dur(s.Uptime))
	fmt.Fprintf(&b, "🖥 CPU %.0f%%  ·  load %.2f %.2f %.2f\n",
		s.CPUTotal.Percent, s.Load.Load1, s.Load.Load5, s.Load.Load15)
	fmt.Fprintf(&b, "🧠 RAM %.0f%% of %s", s.Mem.UsedPercent(), Bytes(s.Mem.Total))
	if s.Mem.SwapTotal > 0 {
		fmt.Fprintf(&b, "  ·  swap %s/%s", Bytes(s.Mem.SwapTotal-s.Mem.SwapFree), Bytes(s.Mem.SwapTotal))
	}
	b.WriteString("\n")
	for _, m := range s.Mounts {
		fmt.Fprintf(&b, "💾 %s %.0f%% used, %s free\n", esc(m.MountPoint), m.UsedPercent, Bytes(m.FreeBytes))
	}
	for _, n := range s.Net {
		fmt.Fprintf(&b, "🌐 %s ↓%s ↑%s (total ↓%s ↑%s)\n",
			esc(n.Name), Rate(n.RxBytesPerSec), Rate(n.TxBytesPerSec), Bytes(n.RxTotal), Bytes(n.TxTotal))
	}
	if len(th.Sensors) > 0 {
		fmt.Fprintf(&b, "🌡 %.0f°C max", th.MaxCelsius())
		if th.Throttled {
			b.WriteString("  ⚠️ throttling")
		}
		b.WriteString("\n")
	}
	if pw.HasBattery {
		for _, bat := range pw.Batteries {
			fmt.Fprintf(&b, "🔋 %.0f%% (%s)", bat.Percent, esc(bat.Status))
			if pw.OnBattery() && bat.TimeToEmpty > 0 {
				fmt.Fprintf(&b, " — ~%s left", Dur(bat.TimeToEmpty))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n%d processes  ·  %d open fds", s.NumProc, s.FileNR.Allocated)
	return b.String()
}

// CPU renders /cpu.
func CPU(s collect.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>CPU</b> %.1f%%  ·  load %.2f %.2f %.2f\n",
		s.CPUTotal.Percent, s.Load.Load1, s.Load.Load5, s.Load.Load15)
	for _, c := range s.PerCore {
		fmt.Fprintf(&b, "%s %s %.0f%%\n", esc(c.Name), bar(c.Percent), c.Percent)
	}
	return b.String()
}

// Mem renders /mem.
func Mem(s collect.Snapshot) string {
	m := s.Mem
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Memory</b> %.1f%%\n", m.UsedPercent())
	fmt.Fprintf(&b, "used %s of %s\navailable %s\nbuffers/cache %s\n",
		Bytes(m.Total-m.Available), Bytes(m.Total), Bytes(m.Available), Bytes(m.Buffers+m.Cached))
	if m.SwapTotal > 0 {
		fmt.Fprintf(&b, "swap %s of %s", Bytes(m.SwapTotal-m.SwapFree), Bytes(m.SwapTotal))
	}
	return b.String()
}

// Disk renders /disk.
func Disk(s collect.Snapshot) string {
	var b strings.Builder
	b.WriteString("<b>Disk</b>\n")
	for _, m := range s.Mounts {
		fmt.Fprintf(&b, "%s %s %.0f%% — %s free of %s\n",
			esc(m.MountPoint), bar(m.UsedPercent), m.UsedPercent, Bytes(m.FreeBytes), Bytes(m.TotalBytes))
	}
	for _, d := range s.Disks {
		if d.ReadBytesPerSec > 0 || d.WriteBytesPerSec > 0 {
			fmt.Fprintf(&b, "%s r %s  w %s  %.0f IOPS\n",
				esc(d.Name), Rate(d.ReadBytesPerSec), Rate(d.WriteBytesPerSec), d.IOPS)
		}
	}
	return b.String()
}

// Net renders /net.
func Net(s collect.Snapshot) string {
	var b strings.Builder
	b.WriteString("<b>Network</b>\n")
	for _, n := range s.Net {
		fmt.Fprintf(&b, "%s ↓%s ↑%s\n   total ↓%s ↑%s\n",
			esc(n.Name), Rate(n.RxBytesPerSec), Rate(n.TxBytesPerSec), Bytes(n.RxTotal), Bytes(n.TxTotal))
	}
	return b.String()
}

// Temp renders /temp.
func Temp(th sysfs.Thermal) string {
	if len(th.Sensors) == 0 {
		return "No thermal sensors found (normal on a VPS)."
	}
	var b strings.Builder
	b.WriteString("<b>Temperatures</b>\n")
	for _, s := range th.Sensors {
		fmt.Fprintf(&b, "%s: %.0f°C\n", esc(s.Label), s.Celsius)
	}
	for _, f := range th.Fans {
		fmt.Fprintf(&b, "%s: %d RPM\n", esc(f.Label), f.RPM)
	}
	if th.Throttled {
		b.WriteString("⚠️ <b>Thermal throttling active</b>")
	}
	return b.String()
}

// Battery renders /battery.
func Battery(pw sysfs.Power) string {
	if !pw.HasBattery {
		return "No battery found (normal on a VPS)."
	}
	var b strings.Builder
	for _, bat := range pw.Batteries {
		fmt.Fprintf(&b, "<b>%s</b> %.0f%% — %s\n", esc(bat.Name), bat.Percent, esc(bat.Status))
		if bat.HealthPercent > 0 {
			fmt.Fprintf(&b, "health %.0f%% of design capacity\n", bat.HealthPercent)
		}
		if bat.Watts > 0 {
			fmt.Fprintf(&b, "draw %.1f W\n", bat.Watts)
		}
		if bat.TimeToEmpty > 0 {
			fmt.Fprintf(&b, "~%s until empty\n", Dur(bat.TimeToEmpty))
		}
	}
	if pw.HasAC {
		if pw.ACOnline {
			b.WriteString("🔌 plugged in")
		} else {
			b.WriteString("⚡ on battery")
		}
	}
	return b.String()
}

// ServiceResults renders /services.
func ServiceResults(results []services.Result) string {
	if len(results) == 0 {
		return "No services configured. Re-run the installer to add some."
	}
	var b strings.Builder
	b.WriteString("<b>Services</b>\n")
	for _, r := range results {
		icon := "✅"
		switch r.State {
		case services.StateDown:
			icon = "❌"
		case services.StateDegraded:
			icon = "⚠️"
		case services.StateUnknown:
			icon = "❔"
		}
		fmt.Fprintf(&b, "%s %s — %s\n", icon, esc(r.Name), esc(r.Detail))
	}
	return b.String()
}

// Docker renders /docker.
func Docker(cts []dockermon.Container) string {
	if len(cts) == 0 {
		return "No containers (or Docker is not running)."
	}
	var b strings.Builder
	b.WriteString("<b>Containers</b>\n")
	for _, c := range cts {
		icon := "✅"
		if c.State != "running" {
			icon = "❌"
		}
		fmt.Fprintf(&b, "%s <b>%s</b> (%s) — %s", icon, esc(c.Name), esc(c.Image), esc(c.Status))
		if c.State == "running" && c.MemLimit > 0 {
			fmt.Fprintf(&b, "\n   cpu %.1f%%  mem %s/%s", c.CPUPercent, Bytes(c.MemUsage), Bytes(c.MemLimit))
		}
		if c.RestartCount > 0 {
			fmt.Fprintf(&b, "\n   restarts: %d", c.RestartCount)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Sessions renders /ssh: live sessions with geo info.
func Sessions(sessions []sshwatch.Session, geo map[string]sshwatch.GeoInfo, now time.Time) string {
	if len(sessions) == 0 {
		return "No active SSH sessions."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Active sessions (%d)</b>\n", len(sessions))
	for _, s := range sessions {
		fmt.Fprintf(&b, "👤 <b>%s</b> on %s from %s — %s",
			esc(s.User), esc(s.TTY), esc(s.Host), Dur(now.Sub(s.Since)))
		if g, ok := geo[s.Host]; ok && g.String() != "" {
			fmt.Fprintf(&b, "\n   📍 %s", esc(g.String()))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// SSHEvents renders /ssh history and /ssh fails.
func SSHEvents(title string, events []sshwatch.Event) string {
	if len(events) == 0 {
		return "Nothing recorded yet."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n", esc(title))
	for _, e := range events {
		icon := map[sshwatch.EventKind]string{
			sshwatch.EventLogin:       "✅",
			sshwatch.EventFailed:      "❌",
			sshwatch.EventInvalidUser: "🚫",
			sshwatch.EventDisconnect:  "👋",
		}[e.Kind]
		fmt.Fprintf(&b, "%s %s <b>%s</b> from %s", icon, e.At.Format("Jan 2 15:04"), esc(e.User), esc(e.IP))
		if e.Method != "" {
			fmt.Fprintf(&b, " (%s)", esc(e.Method))
		}
		if e.InvalidUser {
			b.WriteString(" — invalid user")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// AlertMsg renders a push alert.
func AlertMsg(hostname string, a alert.Alert) string {
	icon := "ℹ️"
	switch {
	case a.Resolved:
		icon = "✅"
	case a.Severity == alert.Critical:
		icon = "🚨"
	case a.Severity == alert.Warning:
		icon = "⚠️"
	}
	return fmt.Sprintf("%s <b>%s</b> — %s\n%s", icon, esc(a.Title), esc(hostname), esc(a.Body))
}

// bar renders a 10-segment usage bar.
func bar(percent float64) string {
	filled := int(percent/10 + 0.5)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", 10-filled)
}
