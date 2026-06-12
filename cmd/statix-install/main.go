// statix-install is the one-time TUI installer (MVP §4): it collects the
// Telegram credentials, monitor toggles, thresholds, and watch lists, then
// writes the config, installs the agent binary, and starts the systemd unit.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eliau2005/statixagent/internal/config"
	"github.com/eliau2005/statixagent/internal/install"
	"github.com/eliau2005/statixagent/internal/services"
)

func main() {
	agentBin := flag.String("agent-binary", "./statix-agent", "path to the statix-agent binary to install")
	prefix := flag.String("prefix", "", "install under this root (testing)")
	noSystemd := flag.Bool("no-systemd", false, "skip systemctl (write files only)")
	flag.Parse()

	if _, err := os.Stat(*agentBin); err != nil {
		fmt.Fprintf(os.Stderr, "agent binary not found at %s (pass --agent-binary)\n", *agentBin)
		os.Exit(1)
	}

	m := newWizard()
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "installer error:", err)
		os.Exit(1)
	}
	w := final.(wizard)
	if w.aborted {
		fmt.Println("Installation cancelled.")
		os.Exit(1)
	}

	cfg, err := w.toConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid input:", err)
		os.Exit(1)
	}
	opts := install.Options{Prefix: *prefix, AgentBinary: *agentBin, Config: cfg}
	if !*noSystemd {
		opts.Runner = services.ExecRunner{}
	}
	res, err := install.Install(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install failed:", err)
		os.Exit(1)
	}
	fmt.Println("\n✅ StatixAgent installed.")
	fmt.Println("  binary:", res.BinaryPath)
	fmt.Println("  config:", res.ConfigPath, "(0600)")
	fmt.Println("  unit:  ", res.UnitPath)
	if res.Started {
		fmt.Println("  service enabled and started — check Telegram for the hello message.")
	} else {
		fmt.Println("  start manually: systemctl enable --now statix-agent.service")
	}
}

// toConfig builds and validates the config from wizard answers.
func (w wizard) toConfig() (config.Config, error) {
	cfg := config.Default()
	cfg.Telegram.Token = strings.TrimSpace(w.answers[qToken])
	chatID, err := strconv.ParseInt(strings.TrimSpace(w.answers[qChatID]), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("chat ID must be a number: %q", w.answers[qChatID])
	}
	cfg.Telegram.ChatID = chatID

	cfg.Monitors.System = w.toggles[0].on
	cfg.Monitors.Thermal = w.toggles[1].on
	cfg.Monitors.Power = w.toggles[2].on
	cfg.Monitors.Docker = w.toggles[3].on
	cfg.Monitors.SSH = w.toggles[4].on
	cfg.Update.Auto = w.toggles[5].on

	for q, dst := range map[int]*float64{
		qCPU:     &cfg.Thresholds.CPUPercent,
		qDisk:    &cfg.Thresholds.DiskPercent,
		qTemp:    &cfg.Thresholds.TempCelsius,
		qBattery: &cfg.Thresholds.BatteryPercent,
	} {
		if v := strings.TrimSpace(w.answers[q]); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return cfg, fmt.Errorf("threshold %q is not a number", v)
			}
			*dst = f
		}
	}

	cfg.Watch.Services = splitList(w.answers[qServices], ".service")
	cfg.Watch.Processes = splitList(w.answers[qProcesses], "")
	for _, p := range splitList(w.answers[qPorts], "") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return cfg, fmt.Errorf("port %q is not a number", p)
		}
		cfg.Watch.Ports = append(cfg.Watch.Ports, config.PortCheck{Port: n})
	}
	return cfg, cfg.Validate()
}

// splitList parses "a, b,c" into trimmed entries, appending suffix when the
// entry does not already end with it.
func splitList(s, suffix string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if suffix != "" && !strings.HasSuffix(part, suffix) {
			part += suffix
		}
		out = append(out, part)
	}
	return out
}
