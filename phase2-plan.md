# Phase 2 Plan: Container as the Core

Status: planned 2026-10-01. Steps 1–6 built 2026-10-01; step 7 (MEX scaffold) remains. The decisions behind this plan are recorded in
`.mex/context/decisions.md` under "Phase 2". Read that file and `.mex/ROUTER.md` before starting.

## Goal

Make Spark useful for teams. The Docker container becomes the core of the app: it owns one
host folder, **SparkRoot**, and everything else reads from or writes to that folder. Later
workflow pieces will plug into SparkRoot too.

## Target shape

```text
host                                         container (owns SparkRoot)
────                                         ──────────────────────────
collector.py (cron)                          web app: dashboard (unchanged) + settings page
  reads  SparkRoot/<settings>  ◄──────────── writes scan roots, project types, colors     
  scans  the scan roots for spark.md
  writes SparkRoot/Projects/  ─────────────► reads snapshots, renders cards

~/.claude/skills/spark ──symlink──► SparkRoot/Skill/
outside sources ◄──read/write──► SparkRoot
```

SparkRoot (for example `~/Documents/Spark`) is a bind mount into the container, run with
`user: "<uid>:<gid>"` so every file stays owned by the host user. Layout:

```text
SparkRoot/
  collector.py  the collector; SparkRoot is the folder it sits in
  Projects/     snapshots, named projectName__projectType.md
  Skill/        SKILL.md, format.md, template.md
  Config/       scan_roots.json, project_types.json, priority_colors.json
```

## Locked decisions

1. No machine ID anywhere.
2. SparkRoot is the single folder everything runs from, with subfolders. Bind mount plus a `user:` line.
3. The container owns SparkRoot and never accesses any other host folder. Outside sources may read and write SparkRoot.
4. The worker (today's collector) runs on the host. It reads scan roots from SparkRoot, finds `spark.md`, then copies and renames it into SparkRoot. Nothing else.
5. Filename: `projectName__projectType.md`, both parts camelCase, built from the snapshot's front matter. On a clash, copy the first, skip the rest, and log a warning.
6. No automatic deletion. To remove a project, delete its file from SparkRoot by hand.
7. Everything that must survive a rebuild lives in SparkRoot, including API keys. Accepted risk: anything that can read SparkRoot can read the keys.
8. The front end stays as it is, plus a settings page for scan roots (add/remove), project types and colors (remote servers: phase 3, see 19).
9. Login is off by default for local use and turned on at setup when requested.
10. Project types are data: edited on the settings page, stored in SparkRoot, read by the skill and the web app.
11. Setup creates the `~/.claude/skills/spark` symlink to `SparkRoot/Skill`.
12. (Q7) Drop the SMB target and the central-server route (Samba + Caddy).
13. (Q8) Keep the name "collector".
14. (Q3) Subfolders are `Projects/`, `Skill/`, `Config/`.
15. (Q4) The collector is a Python 3 script (standard library only) at `SparkRoot/collector.py`, replacing the Go module. SparkRoot is the folder the script sits in, so it needs no config. The user schedules it (cron, every 15 minutes or so).
16. (Q2) Settings are JSON, one file per concern in `Config/`: `scan_roots.json` (list of paths), `project_types.json`, `priority_colors.json`.
17. (Q9) Old `machine__folder.md` files are deleted, not migrated.
18. (Q5) Colors are per deployment and adjustable, for both project types and priorities 1–5. One color per entry, used in light and dark mode. Today's hardcoded colors are the defaults, except two middle-ground values that work in both modes: side-project `#64748b`, just-for-fun `#b8a67e`. Files: `project_types.json` (`[{"name", "color"}]`), `priority_colors.json` (`{"1": "#hex", ...}`).
19. Remote servers move to phase 3 (2026-10-01): phase 2 stays local only. Open question 1 (push, pull or both, and over what protocol) and `remote_servers.json` go with them.
20. (Q6) Setup is both a script and written instructions. The image holds everything: on every start the container copies `collector.py`, `setup.sh`, `INSTALL.md` and `Skill/` into SparkRoot (overwriting them, so a new image updates them), then runs the web app. `Config/` and `Projects/` are never overwritten. `setup.sh`, run once on the host from SparkRoot, does the two host-only steps: the `~/.claude/skills/spark` symlink and the cron line. Login is `-e SPARK_USERNAME/SPARK_PASSWORD` on `docker run`. The user will publish the image to Docker Hub.

## Open questions

All phase 2 questions are answered (see locked decisions 12–20). Remote servers (question 1) moved to phase 3.

## Steps

Estimates assume the open questions for that step are answered.

### 1. Collector (done 2026-10-01)
Files: `collector/collector.py` (replaces `collector/*.go`, `collector.env.example`).
- No `MACHINE_ID`, no prune, no env file, no SMB. The collector never deletes.
- Read scan roots from `Config/scan_roots.json` on each run. Expand `~`. Missing or invalid file: exit 1.
- Write into `SparkRoot/Projects/` (created if missing).
- Name each copy from front matter: camelCase(`project`) + `__` + camelCase(`project_type`) + `.md`. camelCase strips characters that aren't letters or digits and joins words (`Spark / Web App` → `sparkWebApp`, `side-project` → `sideProject`).
- Clash: same target name twice in one run → copy the first, log a warning naming both source paths, skip the rest.
- A `spark.md` with unreadable front matter: log and skip.
- Keep atomic writes (temp file + rename).
- Clash check is case-insensitive (some filesystems ignore case). A copy failure exits 1; a clash or bad front matter is a warning only.
- Still to do in Step 5: `collector/systemd/*` still points at the old Go binary.
- Done when: a run against two test projects produces two correctly named files, a forced clash logs a warning, and nothing is ever deleted.

### 2. Web app: read the new layout (done 2026-10-01)
Built: `SPARK_ROOT` replaces `SPARK_DATA_DIR` (default: the folder the web binary sits in, the collector's rule). `SPARK_MERGE` and machine labels are gone. Type and priority colors come from a generated `GET /colors.css` (public, like `/static/`), so `style.css` no longer holds them. The login button and error box use fixed `--accent`/`--danger` colors. The `format.md`/`template.md`/`SKILL.md` type changes from Step 5 shipped with this step, as the non-negotiable requires.
Files: `web/projects.go`, `web/main.go`, `web/templates/*.html`, `web/static/style.css`.
- Read snapshots from `SparkRoot/Projects/` (`SPARK_DATA_DIR` default changes).
- Stop parsing machine and folder out of the filename; the filename is only the URL id. Name and type come from front matter.
- Remove `SPARK_MERGE`, `Machine`, `Folder`, `ShowMachine` and the machine label in both templates.
- Validate `project_type` against the project-types file in SparkRoot instead of `validType`.
- Done when: the dashboard and project pages render from new-style files, with no machine labels.

### 3. Settings storage (done 2026-10-01)
Built: `web/settings.go`. On start, creates `Projects/`, `Config/` and any missing settings file (`scan_roots.json` starts as `[]`). Invalid type or color entries are logged once and dropped, so one typo doesn't hide every card. `remote_servers.json` is phase 3.
- One small module in the web app to read and write the settings files in SparkRoot, atomically (temp + rename).
- First run: if a settings file is missing, create it with defaults (today's four project types).
- Done when: the web app starts with an empty SparkRoot and creates the default files.

### 4. Settings page (done 2026-10-01; remote servers moved to phase 3)
Built: `GET /settings` (`web/settings_page.go`, `templates/settings.html`), linked from the dashboard header. Posts go to `/settings/scan-roots`, `/settings/types` and `/settings/priorities`. CSRF: a random per-process token in every form, plus a `Sec-Fetch-Site`/`Origin` same-origin check. Scan roots must start with `/` or `~/`, because the container can't check host paths. Edits work on the file as written, so invalid hand-written entries survive for fixing by hand.
Files: new route and template in `web/`; follow `.mex/patterns/add-web-page.md`.
- Scan roots: list, add, remove.
- Project types: list, add, remove (plus color if open question 5 says so).
- POST forms need CSRF protection, since the page now changes state.
- Behind `require` like every other page, so login applies when it is on.
- Done when: a scan root added in the page is picked up on the worker's next run.

### 5. Skill and setup (done 2026-10-01)
Built: `setup.sh` (repo root; symlink, idempotent cron line, last run logged to `SparkRoot/collector.log`). It leaves a real folder at `~/.claude/skills/spark` alone. `SKILL.md` resolves the skill folder's real path before reading `../Config/project_types.json`, since tools may resolve `..` through the symlink as text. `collector/systemd/` removed.
Files: `skill/SKILL.md`, `skill/format.md`, `skill/template.md`, setup script or `INSTALL.md`.
- `format.md` and `template.md`: project types are no longer a fixed list; point to the project-types file in SparkRoot. Change these together with step 2 (existing non-negotiable).
- `SKILL.md`: read allowed types from that file before choosing one.
- Setup: create SparkRoot and its subfolders, put the skill in `SparkRoot/Skill/`, symlink `~/.claude/skills/spark`, install the worker and its timer, optionally set login.
- Done when: a fresh machine can go from nothing to a card on the dashboard by following setup.

### 6. Container and docs (done 2026-10-01)
Built: the image runs only the web app, plus the SparkRoot fill in `docker/entrypoint.sh` (copy + rename, refuses to start if `/spark` isn't writable, warns when run as root). `INSTALL.md` rewritten (central-server and SMB sections removed), README and compose example updated. Tested: fresh SparkRoot → files owned by the host user → setup.sh → root added on the settings page → cron command → card on the dashboard. A restart overwrites `Skill/` and keeps `Config/`.
Files: `Dockerfile`, `docker/entrypoint.sh` (the Docker build is broken until this step: it still builds the Go collector), `docker-compose.example.yml`, `INSTALL.md`, `README.md`.
- Container runs only the web app: remove the collector build, the collector loop, `COLLECT_INTERVAL`, `SCAN_ROOT`, `MACHINE_ID` and `/sources`.
- Mount SparkRoot (for example `~/Documents/Spark:/spark`), with `user:` in the compose example.
- Rewrite `INSTALL.md` around the new shape. Remove or update the central-server section per open question 7.
- Done when: `docker compose up -d` with the example file serves the dashboard from SparkRoot.

### 7. MEX scaffold (about 20 min)
- `.mex/AGENTS.md` non-negotiables: replace "the web app only reads", "never prune on an empty scan" and the machine-id filename scheme with the new rules.
- Update `context/architecture.md`, `context/setup.md`, `context/snapshot-format.md`, `ROUTER.md` current state.
- Mark the phase 2 decisions in `context/decisions.md` as Active (built).
- Update or retire `patterns/deploy-central-server.md` and `patterns/debug-missing-card.md`.
- `patterns/add-web-page.md`: "handlers are GET-only and read-only" no longer holds; document `updateSettings` and `checkPost` for POST routes.

## Phase 3 (not planned yet)

- Remote server connections: add and remove on the settings page, store API keys in SparkRoot (decision 7), never echo a stored key back into the page. First question: push this SparkRoot's snapshots up, pull a team's down, or both, and over what protocol?

## Effects to keep in mind

- Renaming a project or changing its type leaves the old card until its old file is deleted by hand.
- The web app now writes (settings). Writes go only to the settings files, never to snapshots.
- Without login, anyone who can reach the site can change the settings. Keep the port on `127.0.0.1` unless login is on.
- Outside sources that write into SparkRoot should also write atomically, or the web app may briefly read a partial file. It already skips invalid files without crashing.

## Verify (each step)

`go vet ./...` and `gofmt -l .` in each module, plus the step's "Done when" check. There is
no test suite yet.
