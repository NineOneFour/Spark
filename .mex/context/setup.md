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
  - "cron"
  - "SparkRoot"
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
last_updated: 2026-10-02
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

Full user-facing guide: `INSTALL.md` (Docker first, plus a short "without Docker" route). It is also copied into every SparkRoot.

<!-- mex:entity
id: mx_01M3QT590YCF4HB15N75G17C7A
type: guide
status: promoted
revision: 1
-->
## Prerequisites
- Docker (the normal route), or Go 1.27+ to build the web app yourself
- `python3` on the host (the collector, stdlib only) and `crontab` (or another scheduler)
- A coding agent that loads skills from `~/.claude/skills/` (for the skills)
- Development: Go is not required locally; `docker run --rm -u $(id -u):$(id -g) -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -v "$PWD":/src -w /src golang:1.27.1 go vet ./...` in `web/` works without it

<!-- mex:entity
id: mx_01M3QT590QTQQMEA6608V4TFA1
type: guide
status: promoted
revision: 1
-->
## First-time Setup
1. `mkdir -p ~/Documents/Spark` (create SparkRoot yourself; if Docker creates it, root owns it)
2. `docker build -t spark .` then `docker run -d --name spark -p 127.0.0.1:8080:8080 --user "$(id -u):$(id -g)" -v ~/Documents/Spark:/spark spark`
3. The container fills SparkRoot (`collector.py`, `setup.sh`, `INSTALL.md`, `Skill/`, `HandoffSkill/`, `Config/` defaults, `Projects/`)
4. `~/Documents/Spark/setup.sh` on the host: links `~/.claude/skills/spark` → `SparkRoot/Skill` and `~/.claude/skills/spark-handoff` → `SparkRoot/HandoffSkill`, and adds the cron line
5. Add scan roots on http://127.0.0.1:8080/settings, then `python3 ~/Documents/Spark/collector.py` to collect right away

Testing without touching your real setup: point the container at a scratch folder, and run `setup.sh` with `HOME=<scratch>` and a stub `crontab` first on `PATH`.

<!-- mex:entity
id: mx_01M3QT590F1Z0TK14XQ2C16CPP
type: guide
status: promoted
revision: 1
-->
## Environment Variables
Collector: none. SparkRoot is the folder `collector.py` sits in; scan roots come from `Config/scan_roots.json`.

Web (env file only via `-config`; env vars win; all optional):
- `SPARK_ROOT`: default the folder the binary sits in; `/spark` in the image
- `SPARK_ADDR`: default `127.0.0.1:8080`; `:8080` in the image
- `SPARK_USERNAME` + `SPARK_PASSWORD`: set both or neither; neither means login off
- `SPARK_URL`: optional; when set, the only host name accepted, and the source of invite links, the Origin check, the Secure flag and HSTS. A remote warns at start without it
- `SPARK_ALLOW_NETWORK`: local only, default `false` (answers only to `localhost`, `127.0.0.1`, `[::1]`); warns at start when on
- `SPARK_MIN_PASSWORD_LENGTH` (15; `SPARK_PASSWORD` shorter fails startup), `SPARK_PENALTY_START` (4), `SPARK_LOCKOUT_AFTER` (start+7), `SPARK_LOCKOUT` (on): the login penalty schedule in `web/lockout.go`; `web unlock <username>` clears `Config/lockouts.json`. Looser than default logs a warning at start
- `SPARK_TRUSTED_PROXIES`: addresses/ranges whose `X-Forwarded-For`/`-Proto` are believed (`web/edge.go`); default none

Deployment settings (JSON in `SparkRoot/Config/`, created with defaults by the web app): `scan_roots.json`, `project_types.json`, `priority_colors.json`. See `INSTALL.md` "Settings".

<!-- mex:entity
id: mx_01M3QT59096W1D8FD7RV6HWXY8
type: guide
status: promoted
revision: 1
-->
## Common Commands
- `(cd web && go build -o web .)`: build the web app
- `go vet ./...` / `gofmt -l .` in `web/`: only static checks (no tests exist)
- `python3 SparkRoot/collector.py`: run the collector once (it exits 1 if `Config/scan_roots.json` is missing)
- `docker build -t spark .` then `docker run ... -v ~/Documents/Spark:/spark --user "$(id -u):$(id -g)" spark`
- `docker logs spark`: web app log; `SparkRoot/collector.log`: last collector run

<!-- mex:entity
id: mx_01M3QT5902KCPB248BAKK0N03Y
type: guide
status: promoted
revision: 1
-->
## Common Issues
From documented behavior in `INSTALL.md` and the code:

**Container exits at start, "not writable":** Docker created the SparkRoot folder as root, or `--user` is missing. Create the folder first and run with `--user "$(id -u):$(id -g)"`.

**Login cookie not kept on plain HTTP:** the cookie is `Secure` only with TLS or `X-Forwarded-Proto: https`. Behind a proxy, make sure the proxy sends that header.

**Old cards stick around:** nothing is deleted automatically. Delete the file from `SparkRoot/Projects/` by hand.

**Edits to `Skill/`, `HandoffSkill/` or `collector.py` vanish:** the container overwrites them on every start. Customize through `Config/`, or change the repo and rebuild the image.

**`cp` prompts in your shell:** some shells alias `cp` to `cp -i`; use `command cp -f` in scripts and tests.
