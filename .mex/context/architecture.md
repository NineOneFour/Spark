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
- **settings** (`web/settings.go`): `ensureSettings` creates `Projects/`, `Config/` and missing default files on start (`scan_roots.json` only on local, in `newLocalMode`); loaders validate entries (`typeNameRe`, `colorRe`) and log bad ones once; `writeJSON` writes temp + rename.
- **settings page** (`web/settings_page.go`): scan roots, project types, priority colors. `updateSettings` checks the post (`checkPost`: `Sec-Fetch-Site`/`Origin` + a form token: per account with login on, an HMAC of username and password tag under the session key; per process with login off; `csrfToken`), serializes writes under `settingsMu`, and edits the file as written so invalid hand entries survive.
- **mode boundary** (`web/main.go`): `mode` interface (`routes`, `fileKey`, `acceptsType`, `canEdit`, `canEditSettings`, `settingsData`, `stateChanged`), implemented by `localMode` (`local.go`) and `remoteMode` (`remote.go`). Shared routes: `/`, `/p/{key}`, `POST /p/{key}/state`, `/settings`, types, colors, login.
- **project loader** (`web/projects.go`): `s.loadProjects` → `mode.fileKey` → `parseSnapshot` (`splitFrontMatter` + YAML + `parseSections`) → `applyState`. Returns archived files too; `buildCards` drops them and groups by `Key` (`project__type`). Viewer with a file on the card sees its priority; others see the most urgent. Rendering is goldmark then bluemonday (`newSanitizer`).
- **state** (`web/state.go`): `Config/state.json`, map of file id → `priority`, `priority_set`, `archived` (+ `sync` per remote on local). `seedState` from front matter on first sight (`archived` → archived at 5). `changeState` handles the project page form and Settings → Archived.
- **auth** (`web/auth.go`): one login system for both modes. `Config/accounts.json` (bcrypt passwords, SHA-256 API key and invite hashes), gorilla/sessions cookie `spark_session` signed with `Config/session_key.json`, session tied to a password tag. Env account is seeded/updated as admin on start. `POST /logout` (CSRF-checked) clears the cookie. `require` is a no-op when `auth` is nil (local without login) and puts the account in the request context (`viewer`).
- **local sync** (`web/local_sync.go`): `syncLoop` pushes every 60s, on `kick`, and after each 15-minute pull. A file is pushed to each remote in `Config/remotes.json` whose `types` include it, when its hash, priority or archive flag differs from its `syncRecord`; priority/archived are sent only when changed locally. A file archived before its first push is never sent. Pull adopts a remote priority whose stamp is newer, unless a local change is still unpushed. `forgetPushes` drops a remote's records when it is added or removed. "Push everything again" (Settings → Remotes) calls `resetPushes` instead: each existing record is set to differ in every field, so content, priority and archive flag are all re-sent and local wins (a mirror); a file archived before its first push still stays local.
- **remote** (`web/remote*.go`): files `username__project__type.md` (`remoteFileRe`). API behind `requireKey` (`GET /api/types`, `PUT /api/files/{id}`, `GET /api/priorities`; contract in `api.go`); push checks the id (`localFileRe`), size (`maxPush`) and the snapshot against format.md, then writes it and stamps priority with the remote clock. Admin: accepted types (`Config/remote.json`), invites (one-time link, 7 days), removing accounts (`changeAccounts`: login, keys and sessions go; `retire` renames the account's files and `state.json` entries to `deleted-<name>__…`, or `deleted-<name>-2__…` after an earlier removal, with `archive_at` 30 days out, applied by `applyState` on the next load; a hand archive or unarchive clears it; `deleted-` is reserved for invites and `SPARK_USERNAME`; a push writes its file under the state lock after rechecking the account, so it can't race a removal; the env admin can't be removed). `/account`: per-machine API keys shown once. `/invite/{token}`: set password, log in.

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
- **Docker Hub (later phase)**: the image will be published there eventually; for now it is built locally.

<!-- mex:entity
id: mx_01M3QT58W2GTCQXWEHM7T1T090
type: component
status: promoted
revision: 1
-->
## What Does NOT Exist Here
- No database. The only JSON API is the remote's sync API. The web app writes only `Config/*.json`, plus pushed snapshots on remote.
- No automatic deletion anywhere; old snapshots are removed by hand.
- No machine ID, no SMB. Remotes are the same image in remote mode, not a separate server.
- No task tracking or history: each `spark.md` is a disposable present-tense snapshot.
- No roles beyond admin vs. user, no email, no external identity provider, no self sign-up.
- No Git integration: snapshots never contain commit data, and `last_updated` is never derived from Git.
- No CI. Unit tests: `web/render_test.go` (sanitizer, code blocks). End-to-end tests in `web/e2e/` (build tag `e2e`) run only on demand; see `patterns/run-e2e.md`.
