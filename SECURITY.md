# Security model

From [mvp.md §7](mvp.md) — how StatixAgent handles the three sensitive surfaces.

## Bot token at rest

The Telegram token lives in `/etc/statix-agent/config.toml`. The config
writer always creates this file with `0600` permissions (atomic
temp-file + rename), and the installer creates `/etc/statix-agent` root-owned.
The systemd unit grants the service write access to that directory only
(`ProtectSystem=full` + `ReadWritePaths`).

## Inbound command surface

The agent accepts commands **only** from the chat ID in the config; updates
from any other chat are dropped before parsing. State-changing actions
(`/update confirm`) require an explicit second command — `/update` alone never
mutates anything. There are deliberately no remediation commands (kill
session, block IP) in the MVP.

## Update integrity

Whoever controls the update source controls the server, so:

- Releases ship `checksums.txt` signed with an **ed25519** key
  (`checksums.txt.sig`). The public key is compiled into the agent at release
  time via `-ldflags`.
- `Apply` refuses to run when the build carries no public key, when the
  signature does not verify, or when the downloaded binary's SHA256 does not
  match the signed checksum. There is no "skip verification" path.
- The swap is atomic (`rename`) and keeps the previous binary as `.prev`.
- A crash-loop guard runs at startup: three starts within two minutes
  restores `.prev` automatically and consumes it (no version ping-pong).
- Automatic update checks are **off by default**; opting in happens in the
  installer.

## Reporting

Found a vulnerability? Open a GitHub security advisory on this repository
rather than a public issue.
