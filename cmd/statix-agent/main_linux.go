//go:build linux

// statix-agent is the monitoring daemon (MVP §2): one static binary that
// samples the system, watches sshd, and serves a private Telegram bot.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/eliau2005/statixagent/internal/agent"
	"github.com/eliau2005/statixagent/internal/collect"
	"github.com/eliau2005/statixagent/internal/config"
	"github.com/eliau2005/statixagent/internal/dockermon"
	"github.com/eliau2005/statixagent/internal/procfs"
	"github.com/eliau2005/statixagent/internal/services"
	"github.com/eliau2005/statixagent/internal/sshwatch"
	"github.com/eliau2005/statixagent/internal/sysfs"
	"github.com/eliau2005/statixagent/internal/telegram"
)

// version is stamped by the release build (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	cfgPath := flag.String("config", config.DefaultPath, "path to config.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("statix-agent: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tg := telegram.New(cfg.Telegram.Token)
	a := agent.New(cfg, tg, tg, buildSources(ctx, cfg))
	if err := a.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("statix-agent: %v", err)
	}
}

func buildSources(ctx context.Context, cfg config.Config) agent.Sources {
	hostname, _ := os.Hostname()
	src := agent.Sources{
		Hostname: hostname,
		Sample:   sampleLinux,
		Thermal:  func() (sysfs.Thermal, error) { return sysfs.ReadThermal(os.DirFS("/sys")) },
		Power:    func() (sysfs.Power, error) { return sysfs.ReadPower(os.DirFS("/sys")) },
		Sessions: readSessions,
		Runner:   services.ExecRunner{},
		ProcFS: func() ([]services.Result, error) {
			return services.CheckProcesses(os.DirFS("/proc"), cfg.Watch.Processes), nil
		},
		KeyPaths: findAuthorizedKeys(),
	}
	if cfg.Monitors.SSH {
		src.AuthLines = tailAuthLog(ctx)
	}
	if cfg.Monitors.Docker {
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			src.Docker = dockermon.NewUnixSocket("/var/run/docker.sock")
		}
	}
	return src
}

func sampleLinux(ctx context.Context) (collect.Sample, error) {
	s := collect.Sample{At: time.Now()}

	if err := withFile("/proc/stat", func(f *os.File) (err error) {
		s.Stat, err = procfs.ParseStat(f)
		return
	}); err != nil {
		return s, err
	}
	if err := withFile("/proc/meminfo", func(f *os.File) (err error) {
		s.Mem, err = procfs.ParseMemInfo(f)
		return
	}); err != nil {
		return s, err
	}
	withFile("/proc/loadavg", func(f *os.File) (err error) { s.Load, err = procfs.ParseLoadAvg(f); return })
	withFile("/proc/uptime", func(f *os.File) (err error) { s.Uptime, err = procfs.ParseUptime(f); return })
	withFile("/proc/net/dev", func(f *os.File) (err error) { s.Net, err = procfs.ParseNetDev(f); return })
	withFile("/proc/diskstats", func(f *os.File) (err error) {
		all, err := procfs.ParseDiskStats(f)
		if err != nil {
			return err
		}
		names := make([]string, len(all))
		for i, d := range all {
			names[i] = d.Name
		}
		for _, d := range all {
			if !collect.IsPartition(d.Name, names) && !strings.HasPrefix(d.Name, "loop") {
				s.Disks = append(s.Disks, d)
			}
		}
		return nil
	})
	withFile("/proc/sys/fs/file-nr", func(f *os.File) (err error) { s.FileNR, err = procfs.ParseFileNR(f); return })

	if pids, err := filepath.Glob("/proc/[0-9]*"); err == nil {
		s.NumProc = len(pids)
	}
	if mounts, err := collect.ReadMountUsage(); err == nil {
		s.Mounts = mounts
	}
	return s, nil
}

func withFile(path string, f func(*os.File) error) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	return f(fh)
}

func readSessions() ([]sshwatch.Session, error) {
	return withFileResult("/var/run/utmp")
}

func withFileResult(path string) ([]sshwatch.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return sshwatch.ParseUtmp(f)
}

// tailAuthLog streams sshd log lines: journalctl when available, otherwise
// tail -F /var/log/auth.log. The reader goroutine restarts the source if it
// exits while the agent is still running.
func tailAuthLog(ctx context.Context) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		for ctx.Err() == nil {
			if err := runTail(ctx, ch); err != nil && ctx.Err() == nil {
				log.Printf("statix-agent: auth log tail: %v (retrying in 10s)", err)
				select {
				case <-time.After(10 * time.Second):
				case <-ctx.Done():
				}
			}
		}
	}()
	return ch
}

func runTail(ctx context.Context, ch chan<- string) error {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("journalctl"); err == nil {
		cmd = exec.CommandContext(ctx, "journalctl", "-f", "-n", "0", "_COMM=sshd", "_COMM=sshd-session", "--output", "cat")
	} else {
		cmd = exec.CommandContext(ctx, "tail", "-F", "-n", "0", "/var/log/auth.log")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		select {
		case ch <- sc.Text():
		case <-ctx.Done():
		}
	}
	return cmd.Wait()
}

// findAuthorizedKeys collects the key files of root and every /home user.
func findAuthorizedKeys() []string {
	paths := []string{"/root/.ssh/authorized_keys"}
	homes, _ := filepath.Glob("/home/*/.ssh/authorized_keys")
	return append(paths, homes...)
}
