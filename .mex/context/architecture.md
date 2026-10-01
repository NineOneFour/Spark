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
    condition: when setting up SparkRoot, the container, or the host collector
  - target: patterns/debug-missing-card.md
    condition: when a snapshot is not showing up on the dashboard
  - target: patterns/add-web-page.md
    condition: when extending the web app with a new page or route
# Broad overview: keep this empty unless a claim depends on a few specific symbols.
# Entry shape: { node: "function:<tier-1-id>", fingerprint: "mh:64:<hex>" }
# Graph indexed 0 files at setup (Go not indexed), so no grounding is possible yet.
grounds_to: []
last_updated: 2026-10-01
mex:
  id: mx_01M3QT58XQC6BMQHGY0BXGY4WY
  type: architecture
  status: promoted
  revision: 4
  title: architecture
  relations:
    - type: related_to
      target: mx_01M3QT5915NN48SVYHK9KSXFTW
      note: when setting up SparkRoot, the container, or the host collector
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
One Docker image is the core. It owns one host folder, **SparkRoot**, bind-mounted at `/spark`, and never touches any other host folder. Everything connects through files in SparkRoot:

```text
host                                         container (owns SparkRoot)
skill: "Spark, go" → <project>/spark.md      entrypoint: copy collector.py, setup.sh,
cron → SparkRoot/collector.py                  INSTALL.md, Skill/ into SparkRoot
  reads Config/scan_roots.json               web app:
  scans roots for spark.md                     reads Projects/*.md on every request
  writes Projects/projectName__type.md  ──►    reads + writes Config/*.json (settings page)
~/.claude/skills/spark ──symlink──► Skill/     serves /colors.css from Config/
```

- The skill writes a fresh `spark.md` in the project root; it never edits an old one.
- `collector/collector.py` walks each scan root, stops descending once it finds `spark.md`, skips dot-dirs and `node_modules`/`vendor`/`build`/`dist`, and names each copy from front matter. It never deletes.
- `web/projects.go` rereads `Projects/` and `project_types.json` on every page view; there is no cache or index.
- The shared contracts are `skill/format.md`, the filename scheme, and the JSON files in `Config/`.

<!-- mex:entity
id: mx_01M3QT58X8TZ6FE601S6B5N68F
type: component
status: promoted
revision: 1
-->
## Key Components
- **skill/** (`SKILL.md`, `format.md`, `template.md`): agent instructions that generate `spark.md`. Shipped in the image, copied to `SparkRoot/Skill/`, symlinked from `~/.claude/skills/spark` by `setup.sh`. Reads allowed types from `../Config/project_types.json` via its real path.
- **collector** (`collector/collector.py`): Python 3 stdlib script, run on the host by cron. `SPARK_ROOT` is the script's own folder. `scan`, `front_matter` (flat `key: value`), `camel_case`, `target_name`, atomic `put` (mkstemp + `os.replace`, mode 0644). Case-insensitive clash check: first path wins, rest logged. Exits 1 on a copy failure or a missing `scan_roots.json`.
- **entrypoint** (`docker/entrypoint.sh`): refuses to start if `/spark` isn't writable, warns when run as root, overwrites the shipped files in SparkRoot (copy + rename), then `exec web`.
- **setup.sh** (repo root, shipped into SparkRoot): host-only steps, idempotent. Symlinks the skill (leaves a real folder alone), adds the cron line (`> collector.log`).
- **web server** (`web/main.go`): `net/http` mux with Go 1.22 patterns (`GET /{$}`, `GET /p/{id}`, `GET /settings`, `POST /settings/*`, `/login`, public `GET /colors.css`), embedded templates/static, `securityHeaders` (CSP, nosniff).
- **settings** (`web/settings.go`): `ensureSettings` creates `Projects/`, `Config/` and missing default files on start; loaders validate entries (`typeNameRe`, `colorRe`) and log bad ones once; `writeJSON` writes temp + rename.
- **settings page** (`web/settings_page.go`): scan roots, project types, priority colors. `updateSettings` checks the post (`checkPost`: `Sec-Fetch-Site`/`Origin` + per-process CSRF token), serializes writes under `settingsMu`, and edits the file as written so invalid hand entries survive.
- **project loader** (`web/projects.go`): `loadProjects` → `parseFile` → `splitFrontMatter` + YAML + `parseSections`; drops invalid and `archived` files and types not in `project_types.json`. The filename without `.md` is only the URL id.
- **auth** (`web/auth.go`): optional single-user login; stateless HMAC cookie `spark_session` keyed by username+password; `require` is a no-op when `auth` is nil.

<!-- mex:entity
id: mx_01M3QT58X08DZWHQFZMHB8FT1E
type: component
status: promoted
revision: 1
-->
## External Dependencies
- **SparkRoot** (bind mount, for example `~/Documents/Spark:/spark`, run with `--user uid:gid`): the only storage. `Projects/` snapshots, `Config/` settings, plus the shipped files.
- **cron on the host**: runs the collector every 15 minutes (`setup.sh` adds the line).
- **Caddy or another proxy (optional)**: its `X-Forwarded-Proto: https` makes the session cookie `Secure`.
- **Google Fonts**: allowed by the CSP (`fonts.googleapis.com`, `fonts.gstatic.com`) for the stylesheet.
- **Docker Hub (planned)**: the user will publish the image there.

<!-- mex:entity
id: mx_01M3QT58W2GTCQXWEHM7T1T090
type: component
status: promoted
revision: 1
-->
## What Does NOT Exist Here
- No database, no JSON API. The web app writes only `Config/*.json`, never snapshots.
- No automatic deletion anywhere; old snapshots are removed by hand.
- No machine ID, no SMB, no central-server route (removed in phase 2); remote servers are phase 3.
- No task tracking or history: each `spark.md` is a disposable present-tense snapshot.
- No multi-user accounts or roles: at most one username/password pair.
- No Git integration: snapshots never contain commit data, and `last_updated` is never derived from Git.
- No automated tests or CI yet.
