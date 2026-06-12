package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	c := Default()
	c.Telegram.Token = "123456:ABC-test"
	c.Telegram.ChatID = 42
	return c
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	in := validConfig()
	in.Watch.Services = []string{"nginx.service", "postgresql.service"}
	in.Watch.Ports = []PortCheck{{Port: 443, Label: "https"}}
	in.Watch.HTTPChecks = []HTTPCheck{{URL: "https://example.com/health", ExpectStatus: 200, Timeout: Duration{5 * time.Second}}}
	in.SampleInterval = Duration{30 * time.Second}

	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Telegram != in.Telegram {
		t.Errorf("telegram mismatch: got %+v want %+v", out.Telegram, in.Telegram)
	}
	if out.SampleInterval.Duration != 30*time.Second {
		t.Errorf("sample_interval = %s, want 30s", out.SampleInterval)
	}
	if len(out.Watch.Services) != 2 || out.Watch.Services[0] != "nginx.service" {
		t.Errorf("services = %v", out.Watch.Services)
	}
	if len(out.Watch.HTTPChecks) != 1 || out.Watch.HTTPChecks[0].Timeout.Duration != 5*time.Second {
		t.Errorf("http_checks = %+v", out.Watch.HTTPChecks)
	}
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits not meaningful on windows")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, validConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
sample_interval = "15s"
[telegram]
token = "t"
chat_id = 1
[telegrm]
typo = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("err = %v, want unknown-keys error", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"missing token", func(c *Config) { c.Telegram.Token = "" }, "telegram.token"},
		{"missing chat id", func(c *Config) { c.Telegram.ChatID = 0 }, "telegram.chat_id"},
		{"interval too small", func(c *Config) { c.SampleInterval = Duration{time.Millisecond} }, "sample_interval"},
		{"bad port", func(c *Config) { c.Watch.Ports = []PortCheck{{Port: 70000}} }, "out of range"},
		{"http check without url", func(c *Config) { c.Watch.HTTPChecks = []HTTPCheck{{}} }, "missing url"},
		{"auto update too frequent", func(c *Config) {
			c.Update.Auto = true
			c.Update.CheckInterval = Duration{time.Second}
		}, "check_interval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(&c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
	if err := validConfig().Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestDefaultsAreSane(t *testing.T) {
	d := Default()
	if !d.Monitors.System || !d.Monitors.SSH {
		t.Error("core monitors must default on")
	}
	if d.Update.Auto {
		t.Error("auto-update must default off (MVP §5)")
	}
}
