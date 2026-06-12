package services

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ExecRunner is the production Runner: it executes the command with a
// safety timeout and returns combined output.
type ExecRunner struct {
	// Timeout bounds each invocation; zero means 10s.
	Timeout time.Duration
}

// Run implements Runner.
func (e ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
