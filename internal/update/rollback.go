package update

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// RollbackGuard implements MVP §5's crash-loop rollback. The agent calls it
// first thing on startup: it appends the current start time to a state file
// and, when too many starts cluster inside the window while a .prev binary
// exists, restores the previous binary and reports that a rollback happened
// (the caller should exit and let systemd start the restored version).
type RollbackGuard struct {
	StatePath  string // e.g. /etc/statix-agent/starts
	BinaryPath string
	Window     time.Duration // default 2m
	MaxStarts  int           // default 3
}

// Check records this start and rolls back if a crash loop is detected.
// It returns true when a rollback was performed.
func (g RollbackGuard) Check(now time.Time) (bool, error) {
	window := g.Window
	if window <= 0 {
		window = 2 * time.Minute
	}
	maxStarts := g.MaxStarts
	if maxStarts <= 0 {
		maxStarts = 3
	}

	starts := g.readStarts()
	cutoff := now.Add(-window)
	recent := starts[:0]
	for _, t := range starts {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	g.writeStarts(recent)

	if len(recent) < maxStarts {
		return false, nil
	}
	prev := g.BinaryPath + ".prev"
	prevData, err := os.ReadFile(prev)
	if err != nil {
		return false, nil // crash loop but nothing to roll back to
	}
	if err := SwapBinary(g.BinaryPath, prevData); err != nil {
		return false, fmt.Errorf("update: rollback: %w", err)
	}
	os.Remove(prev)    // consumed: do not ping-pong between versions
	g.writeStarts(nil) // fresh slate for the restored binary
	return true, nil
}

func (g RollbackGuard) readStarts() []time.Time {
	data, err := os.ReadFile(g.StatePath)
	if err != nil {
		return nil
	}
	var out []time.Time
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			out = append(out, time.Unix(n, 0))
		}
	}
	return out
}

func (g RollbackGuard) writeStarts(starts []time.Time) {
	var b strings.Builder
	for _, t := range starts {
		fmt.Fprintf(&b, "%d\n", t.Unix())
	}
	os.WriteFile(g.StatePath, []byte(b.String()), 0o600)
}
