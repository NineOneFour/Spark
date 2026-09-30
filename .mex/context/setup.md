---
name: setup
description: Dev environment setup and commands. Load when setting up the project for the first time or when environment issues arise.
triggers:
  - "setup"
  - "install"
  - "environment"
  - "getting started"
  - "how do I run"
  - "local development"
  - "docker"
  - "systemd"
edges:
  - target: context/stack.md
    condition: when specific technology versions or library details are needed
  - target: context/architecture.md
    condition: when understanding how components connect during setup
  - target: patterns/add-config-setting.md
    condition: when adding a new environment variable
  - target: patterns/debug-missing-card.md
    condition: when setup runs but cards do not appear
grounds_to: []
last_updated: 2026-09-29
mex:
  id: mx_01M3QT5915NN48SVYHK9KSXFTW
  type: guide
  status: promoted
  revision: 4
  title: setup
  relations:
    - type: related_to
      target: mx_01M3QT58XQC6BMQHGY0BXGY4WY
      note: when understanding how components connect during setup
    - type: related_to
      target: mx_01M3QT591CME72XWHMVWM94MNV
      note: when adding a new environment variable
    - type: related_to
      target: mx_01M3QT592371G6KZQ0CGJ65X5K
      note: when setup runs but cards do not appear
---

# Setup

Full user-facing guide: `INSTALL.md` (localhost, Docker, central server routes).

<!-- mex:entity
id: mx_01M3QT590YCF4HB15N75G17C7A
type: guide
status: promoted
revision: 1
-->
## Prerequisites
- Go 1.23+ (to build locally; Docker builds for you)
- A coding agent that loads skills from `~/.claude/skills/` (for the skill)
- Optional: Docker; Samba + Caddy for the central-server route

<!-- mex:entity
id: mx_01M3QT590QTQQMEA6608V4TFA1
type: guide
status: promoted
revision: 1
-->
## First-time Setup
1. `ln -s "$PWD/skill" ~/.claude/skills/spark` (installs the skill)
2. `(cd collector && go build -o collector .)`
3. `(cd web && go build -o web .)`
4. `SCAN_ROOT=~/Projects ./collector/collector`. It writes to `collector/projects/` by default (next to the binary).
5. `SPARK_DATA_DIR=collector/projects ./web/web`, then open http://127.0.0.1:8080

Tip: copy both binaries into one folder (for example `~/spark/`) so their default `projects/` dirs coincide and no config is needed.

<!-- mex:entity
id: mx_01M3QT590F1Z0TK14XQ2C16CPP
type: guide
status: promoted
revision: 1
-->
## Environment Variables
Collector (env file default: `~/.config/spark/collector.env`; env vars win):
- `SCAN_ROOT` (required) — comma-separated roots; `~` expanded
- `MACHINE_ID` (optional, default `local`) — no `__` or path separators; unique per machine
- `TARGET_DIR` (optional) — local destination; default `projects/` next to the binary
- `SMB_HOST` (optional) — switches to SMB; then `SMB_SHARE`, `SMB_USER`, `SMB_PASSWORD` are required; port defaults to 445

Web (env file only via `-config`; env vars win; all optional):
- `SPARK_DATA_DIR` — default `projects/` next to the binary
- `SPARK_ADDR` — default `127.0.0.1:8080`
- `SPARK_USERNAME` + `SPARK_PASSWORD` — set both or neither; neither means login off
- `SPARK_MERGE` — comma-separated folder names merged across machines (newest `last_updated` wins)

Docker only: `COLLECT_INTERVAL` (seconds, default 900; `0` disables the collector loop).

<!-- mex:entity
id: mx_01M3QT59096W1D8FD7RV6HWXY8
type: guide
status: promoted
revision: 1
-->
## Common Commands
- `(cd collector && go build -o collector .)` — build collector
- `(cd web && go build -o web .)` — build web app
- `go vet ./...` / `gofmt -l .` in each module — only static checks (no tests exist)
- `docker build -t spark .` then `docker run ... -v ~/spark-data:/projects -v ~/code:/sources/code:ro spark`
- `systemctl --user enable --now spark-collector.timer` — schedule the collector every 15 min
- `journalctl --user -u spark-collector` — collector logs

<!-- mex:entity
id: mx_01M3QT5902KCPB248BAKK0N03Y
type: guide
status: promoted
revision: 1
-->
## Common Issues
From documented behavior in `INSTALL.md` and the code (no issue history yet):

**Docker collector can't write:** Docker created the data folder as root. Create it first and run with `--user "$(id -u):$(id -g)"`.

**Login cookie not kept on plain HTTP:** the cookie is `Secure` only with TLS or `X-Forwarded-Proto: https`. Behind a proxy, make sure the proxy sends that header.

**Collector deletes nothing / cards stick around:** a scan that finds zero files skips pruning on purpose. Check `SCAN_ROOT`.

**Web and collector disagree on folder:** both default to `projects/` next to their *own* binary. Set `TARGET_DIR` / `SPARK_DATA_DIR` explicitly when the binaries live apart.
