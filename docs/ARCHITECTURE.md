# StatixAgent — Architecture

One static Go binary per server. No central server, no third-party data path
(metrics go straight from the agent to the owner's private Telegram bot).

## Process model

```
statix-agent (single process, systemd-managed, Restart=always)
│
├─ sampler goroutine        — ticks every N seconds, builds a Snapshot from collectors
├─ ssh-watch goroutine      — tails auth log (journalctl/auth.log), emits security events
├─ alert goroutine          — evaluates snapshots + events against rules, dedupes, pushes
├─ bot goroutine            — Telegram getUpdates long-poll, routes commands, replies
└─ update goroutine (opt-in)— periodic GitHub Releases check, verified atomic self-swap
```

All goroutines communicate over channels; a context cancel from SIGTERM/SIGINT shuts
everything down gracefully. Every goroutine runs inside a panic-recovering wrapper so
one failing subsystem cannot kill the agent.

## Package layout

```
cmd/
  statix-agent/      daemon entry point
  statix-install/    one-time TUI installer (Bubble Tea)
internal/
  config/            TOML config: load, validate, save (0600)
  procfs/            pure parsers for /proc files (io.Reader → structs)
  sysfs/             pure parsers for /sys (thermal, hwmon, power_supply)
  collect/           Collector interface, Snapshot model, delta/rate math
  services/          systemd units, process presence, port + HTTP checks
  dockermon/         Docker Engine API over unix socket (no SDK)
  netcheck/          SSL certificate expiry
  sshwatch/          auth log parser, brute-force detector, utmp sessions,
                     authorized_keys watcher, geo-IP cache
  alert/             rules, hysteresis, cooldown, severity, event alerts
  telegram/          minimal Bot API client (sendMessage, getUpdates)
  bot/               command router + message formatting
  update/            release check, SHA256+ed25519 verify, atomic swap, rollback
```

## Portability strategy

The agent targets Linux, but development and CI must run anywhere:

- **Parsers are pure.** Everything that reads `/proc`, `/sys`, auth logs, utmp, or
  Docker API JSON is written as a function over `io.Reader`/`[]byte` and unit-tested
  with fixture files on any OS.
- **OS access is thin.** Files behind `//go:build linux` only *open* the real paths and
  call the pure parsers. Syscall-dependent pieces (statfs) get a small linux-only shim.
- **External commands and sockets sit behind interfaces** (`systemctl`, Docker socket,
  Telegram HTTP) with fakes for tests.

## Data flow

```
/proc, /sys ──► procfs/sysfs parsers ──► collect.Snapshot ──┐
journald/auth.log ──► sshwatch events ─────────────────────┼──► alert engine ──► telegram push
docker.sock ──► dockermon ──────────────────────────────────┘
                                          ▲
user command ──► telegram getUpdates ──► bot router ── reads latest Snapshot / history
```

The bot answers queries from the most recent snapshot plus ring buffers of history
(SSH events, alerts) kept in memory — no database. Restart loses history; that is an
accepted MVP trade-off consistent with the featherweight goal.

## Security posture (MVP §7)

- Config written `0600`; contains the bot token.
- Inbound commands accepted **only** from the configured chat ID.
- State-changing commands (`/update confirm`) require explicit confirmation.
- Self-update verifies SHA256 **and** an ed25519 signature with a public key compiled
  into the binary before the atomic swap; a crash-loop after update triggers rollback
  to the previous binary kept alongside.
- systemd unit ships with hardening directives (`ProtectSystem=strict` where viable,
  `NoNewPrivileges=yes`, read-only paths plus the few required read paths).
