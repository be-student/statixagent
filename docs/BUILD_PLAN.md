# StatixAgent — Build Plan

Derived from [mvp.md](../mvp.md). Each phase ends in a green test suite and a commit.
Development happens on Windows; the target platform is Linux (amd64/arm64). Everything
that parses text (`/proc` formats, logs, config) is platform-neutral and unit-tested with
fixtures; everything that touches the live OS sits behind an interface and `//go:build linux`.

## Status legend

- [ ] not started  · [~] in progress  · [x] done

## Phase 0 — Foundation

- [x] Git repo, MVP spec committed
- [x] Build plan + architecture docs
- [x] Go module `github.com/eliau2005/statixagent`, repo layout, Makefile, lint config
- [x] `internal/config`: TOML config schema (bot token, chat ID, enabled monitors,
      thresholds, services/ports/endpoints lists, auto-update flag), load/validate/save,
      0600 permission enforcement on save. Unit tests.

## Phase 1 — Metric collection (read side)

- [x] `internal/procfs`: parsers for `/proc/stat` (CPU), `/proc/meminfo`, `/proc/loadavg`,
      `/proc/uptime`, `/proc/net/dev`, `/proc/diskstats`, `/proc/sys/fs/file-nr`,
      process count from `/proc/[pid]`. Pure functions over `io.Reader` — fixture tests.
- [x] `internal/collect`: snapshot model (`Snapshot` struct with CPU/mem/disk/net/uptime
      sections). Delta-based rate computation (CPU %, net B/s, disk IOPS) between
      consecutive samples, counter-reset safety, partition filtering. Unit tests.
- [x] `internal/sysfs` (thermal): `/sys/class/thermal` + `/sys/class/hwmon` parsing
      (temp, fans), throttle detection. fstest.MapFS fixture tests.
- [x] `internal/sysfs` (power): `/sys/class/power_supply` parsing — battery %, AC online,
      health (full vs design), wattage, time-to-empty estimate. Fixture tests.
- [x] Disk usage per mount (statfs) behind `//go:build linux`; mount filtering logic
      (skip pseudo-FS, dedupe bind mounts, octal unescape) is platform-neutral and tested.

## Phase 2 — Services, Docker, reachability

- [x] `internal/services`: systemd unit status via `systemctl show` invocation (interface
      + fake for tests), specific-process presence via /proc scan, TCP port checks,
      HTTP healthchecks with timeout + expected-status.
- [x] `internal/dockermon`: Docker Engine API over unix socket (no SDK dependency —
      plain HTTP client): container list, per-container stats, restart-loop detection
      (restart count delta). Fixture tests against recorded API JSON.
- [x] `internal/netcheck`: SSL certificate expiry checks (+ TCP latency helper).

## Phase 3 — SSH & security monitoring

- [x] `internal/sshwatch`: auth-log line parser (sshd journald/auth.log formats):
      accepted logins (user, IP, method key/password), failed attempts, invalid users,
      root logins, disconnects. Pure parser + fixture tests. Event ring buffer (History).
- [x] Brute-force detector: sliding-window counter per IP with threshold alert and
      quiet-period dedupe during ongoing attacks. Unit tests.
- [x] Live sessions via utmp parsing (`/var/run/utmp`) — binary format reader, fixture test.
- [x] `authorized_keys` change watcher (hash polling). Unit tests with temp dirs.
- [x] Geo-IP: offline-friendly — ip-api.com lookup with cache, private-IP short-circuit,
      graceful no-network fallback.

## Phase 4 — Alert engine

- [x] `internal/alert`: threshold rules (CPU %, mem %, disk %, temp, battery %),
      hysteresis (fire once, clear on recovery), cooldown/dedupe, severity levels.
      Power-loss and new-SSH-login as event alerts (no threshold). Unit tests.

## Phase 5 — Telegram bot

- [ ] `internal/telegram`: minimal Bot API client (sendMessage, getUpdates long-poll) —
      no third-party SDK. Chat-ID allowlist (only the configured chat may command).
- [ ] `internal/bot`: command router — `/status`, `/cpu`, `/mem`, `/disk`, `/net`,
      `/temp`, `/battery`, `/services`, `/docker`, `/ssh [history|fails]`,
      `/update [confirm]`. Formatting helpers (HTML messages). Unit tests on the router
      and formatters with a fake transport.
- [ ] Push alerts wired from the alert engine to the bot sender.

## Phase 6 — Agent daemon

- [ ] `cmd/statix-agent`: main loop — goroutines for sampler, bot listener, ssh watcher,
      alert evaluator; graceful shutdown on SIGTERM; panic-safe goroutine wrapper.
- [ ] systemd unit file template (`Restart=always`, hardening directives).

## Phase 7 — Installer TUI

- [ ] `cmd/statix-install` with Bubble Tea: token/chat-ID entry (with guidance),
      monitor toggles, thresholds, service/port selection, auto-update opt-in,
      write config (0600), install binary, create+start systemd unit.
- [ ] `install.sh` one-liner entry script (download release for arch, run installer).

## Phase 8 — Self-update

- [ ] `internal/update`: GitHub Releases check, download to temp, SHA256 + ed25519
      signature verify, atomic rename, systemd restart, crash-loop rollback marker.
      Unit tests for version compare, checksum/signature verification, swap logic.
- [ ] Bot `/update` + `/update confirm` integration.

## Phase 9 — Hardening & release

- [ ] End-to-end smoke build for linux/amd64 + linux/arm64.
- [ ] README with install instructions; SECURITY.md notes from MVP §7.
- [ ] GitHub Actions release workflow (build, checksum, sign, release).

## Out of scope (per MVP §8)

Central aggregator, live TUI dashboard, cross-server correlation, remediation actions.
