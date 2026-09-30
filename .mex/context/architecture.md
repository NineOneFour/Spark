---
name: architecture
description: How the major pieces of this project connect and flow. Load when working on system design, integrations, or understanding how components interact.
triggers:
  - "architecture"
  - "system design"
  - "how does X connect to Y"
  - "integration"
  - "flow"
  - "collector"
  - "dashboard"
edges:
  - target: context/stack.md
    condition: when specific technology details are needed
  - target: context/decisions.md
    condition: when understanding why the architecture is structured this way
  - target: context/snapshot-format.md
    condition: when the task touches what goes inside spark.md or how the web app parses it
  - target: context/setup.md
    condition: when choosing or configuring a deployment route (localhost, Docker, central server)
  - target: patterns/debug-missing-card.md
    condition: when a snapshot is not showing up on the dashboard
  - target: patterns/add-web-page.md
    condition: when extending the web app with a new page or route
# Broad overview: keep this empty unless a claim depends on a few specific symbols.
# Entry shape: { node: "function:<tier-1-id>", fingerprint: "mh:64:<hex>" }
# Graph indexed 0 files at setup (Go not indexed), so no grounding is possible yet.
grounds_to: []
last_updated: 2026-09-29
mex:
  id: mx_01M3QT58XQC6BMQHGY0BXGY4WY
  type: architecture
  status: promoted
  revision: 4
  title: architecture
  relations:
    - type: related_to
      target: mx_01M3QT5915NN48SVYHK9KSXFTW
      note: when choosing or configuring a deployment route (localhost, Docker,
        central server)
    - type: related_to
      target: mx_01M3QT592371G6KZQ0CGJ65X5K
      note: when a snapshot is not showing up on the dashboard
    - type: related_to
      target: mx_01M3QT591MQX0WQ8C6P6K8YGGY
      note: when extending the web app with a new page or route
---

# Architecture

<!-- mex:entity
id: mx_01M3QT58XFJMTF98V119CV62C6
type: component
status: promoted
revision: 1
-->
## System Overview
Three independent pieces connected only by files on disk:

```text
skill (agent, per project)          collector (Go, runs once, on a timer)        web (Go, long-running)
"Spark, go" → <project>/spark.md → scan SCAN_ROOTs → copy as               → loadProjects() on every
                                    <MACHINE_ID>__<folder>.md into              request → parse front
                                    TARGET_DIR or an SMB share → prune          matter + sections → render
                                    this machine's stale files                  cards / project page
```

- The skill writes a fresh `spark.md` in the project root; it never edits an old one.
- `collector/main.go` walks each root, stops descending once it finds `spark.md`, skips dot-dirs and `node_modules`/`vendor`/`build`/`dist`, then copies through a `target` (`collector/target.go`).
- `web/projects.go` rereads the whole data directory on every page view; there is no cache or index.
- The only shared contract is the filename scheme and `skill/format.md`.

<!-- mex:entity
id: mx_01M3QT58X8TZ6FE601S6B5N68F
type: component
status: promoted
revision: 1
-->
## Key Components
- **skill/** (`SKILL.md`, `format.md`, `template.md`) — agent instructions that generate `spark.md`; symlinked into `~/.claude/skills/spark`. `format.md` is also the web parser's spec.
- **collector** (`collector/main.go`) — config loading (env file + env override), `scan`, `copyTo`, `prune`. Exits 1 if any copy or prune fails. Never prunes when a scan finds zero files.
- **target interface** (`collector/target.go`) — `Put/List/Remove/Close`; `localTarget` (write temp, rename) and `smbTarget` (write temp, remove old, rename, because SMB rename won't overwrite). `Put` must never leave a half-written file.
- **web server** (`web/main.go`) — `net/http` mux with Go 1.22 patterns (`GET /{$}`, `GET /p/{id}`, `/login`), embedded templates/static, `securityHeaders` middleware (CSP, nosniff).
- **project loader** (`web/projects.go`) — `loadProjects` → `parseFile` → `splitFrontMatter` + YAML + `parseSections`; drops invalid and `archived` files; applies `SPARK_MERGE`; sets `ShowMachine` when folder names collide.
- **auth** (`web/auth.go`) — optional single-user login; stateless HMAC cookie `spark_session` keyed by username+password; `require` is a no-op when `auth` is nil.

<!-- mex:entity
id: mx_01M3QT58X08DZWHQFZMHB8FT1E
type: component
status: promoted
revision: 1
-->
## External Dependencies
- **Samba/SMB share** — central-server transport; collector connects with NTLM via `go-smb2`, default port 445. LAN only; never expose 445.
- **Filesystem data dir** (`projects/` next to the binaries by default, `/projects` in Docker) — the only storage. Web needs read access only.
- **Caddy (optional reverse proxy)** — HTTPS for the central-server route; its `X-Forwarded-Proto: https` makes the session cookie `Secure`.
- **Google Fonts** — allowed by the CSP (`fonts.googleapis.com`, `fonts.gstatic.com`) for the stylesheet.
- **systemd user timer / Docker loop** — schedules the collector (every 15 min / `COLLECT_INTERVAL`, default 900s).

<!-- mex:entity
id: mx_01M3QT58W2GTCQXWEHM7T1T090
type: component
status: promoted
revision: 1
-->
## What Does NOT Exist Here
- No database, no JSON API, no write path in the web app.
- No task tracking or history: each `spark.md` is a disposable present-tense snapshot.
- No multi-user accounts or roles: at most one username/password pair.
- No Git integration: snapshots never contain commit data, and `last_updated` is never derived from Git.
- No automated tests or CI yet.
