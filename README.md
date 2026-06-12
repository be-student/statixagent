# StatixAgent

Self-hosted, featherweight monitoring for VPS servers and home laptops. One static Go
binary per machine, one private Telegram bot per machine — metrics, service health,
battery/power events, and SSH security alerts, with no central server and no third
party in the data path.

- **Spec:** [mvp.md](mvp.md)
- **Plan:** [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md)
- **Architecture:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Status

Early development — see the build plan for phase-by-phase progress.

## Development

Requires Go ≥ 1.25. The agent targets Linux; the test suite runs on any OS
(all parsers are pure and fixture-tested).

```sh
go test ./...                       # run everywhere
GOOS=linux GOARCH=amd64 go build ./cmd/...   # cross-compile the real thing
```
