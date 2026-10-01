# Phase 2 Plan: Container as the Core

Status: planned 2026-10-01, nothing built yet. The decisions behind this plan are recorded in
`.mex/context/decisions.md` under "Phase 2". Read that file and `.mex/ROUTER.md` before starting.

## Goal

Make Spark useful for teams. The Docker container becomes the core of the app: it owns one
host folder, **SparkRoot**, and everything else reads from or writes to that folder. Later
workflow pieces will plug into SparkRoot too.

## Target shape

```text
host                                         container (owns SparkRoot)
────                                         ──────────────────────────
worker (systemd timer)                       web app: dashboard (unchanged) + settings page
  reads  SparkRoot/<settings>  ◄──────────── writes scan roots, project types, remote servers
  scans  the scan roots for spark.md
  writes SparkRoot/Projects/  ─────────────► reads snapshots, renders cards

~/.claude/skills/spark ──symlink──► SparkRoot/skill/
outside sources ◄──read/write──► SparkRoot
```

SparkRoot (for example `~/Documents/Spark`) is a bind mount into the container, run with
`user: "<uid>:<gid>"` so every file stays owned by the host user. Subfolder names are
working names:

```text
SparkRoot/
  Projects/   snapshots, named projectName__projectType.md
  skill/      SKILL.md, format.md, template.md
  config/     settings files (scan roots, project types, remote servers + API keys)
```

## Locked decisions

1. No machine ID anywhere.
2. SparkRoot is the single folder everything runs from, with subfolders. Bind mount plus a `user:` line.
3. The container owns SparkRoot and never accesses any other host folder. Outside sources may read and write SparkRoot.
4. The worker (today's collector) runs on the host. It reads scan roots from SparkRoot, finds `spark.md`, then copies and renames it into SparkRoot. Nothing else.
5. Filename: `projectName__projectType.md`, both parts camelCase, built from the snapshot's front matter. On a clash, copy the first, skip the rest, and log a warning.
6. No automatic deletion. To remove a project, delete its file from SparkRoot by hand.
7. Everything that must survive a rebuild lives in SparkRoot, including API keys. Accepted risk: anything that can read SparkRoot can read the keys.
8. The front end stays as it is, plus a settings page for scan roots (add/remove), project types, and remote server connections.
9. Login is off by default for local use and turned on at setup when requested.
10. Project types are data: edited on the settings page, stored in SparkRoot, read by the skill and the web app.
11. Setup creates the `~/.claude/skills/spark` symlink to `SparkRoot/skill`.

## Open questions

Answer each one before the step it blocks. Ask the user one at a time.

| # | Question | Blocks |
|---|---|---|
| 1 | What does a remote server connection do: push this SparkRoot's snapshots up, pull a team's down, or both? Over what protocol? | Step 4 (remote part) |
| 2 | Settings file format and layout: one file or several, JSON or YAML? (The user's example `{"~/Projects", "~/Documents/Coding"}` suggests JSON.) | Steps 1, 3 |
| 3 | Final SparkRoot subfolder names | Step 1 |
| 4 | How the worker finds SparkRoot: env var (`SPARK_ROOT`) in its env file? | Step 1 |
| 5 | Does a project type carry a color? Colors are hardcoded per type in `web/static/style.css` today. | Steps 2, 4 |
| 6 | Setup: a script or documented steps? Who puts the skill into `SparkRoot/skill/`, and how does it get updated? | Step 5 |
| 7 | Keep the SMB target and the central-server route, or drop them? | Steps 1, 6 |
| 8 | Rename "collector" to "worker" in code, binaries and docs? | Step 1 |
| 9 | Existing `machine__folder.md` files: migrate or just delete? | Step 1 |

## Steps

Estimates assume the open questions for that step are answered.

### 1. Worker (about 45 min)
Files: `collector/main.go`, `collector/collector.env.example`, `collector/systemd/*`.
- Remove `MACHINE_ID` from config, validation, filenames and comments.
- Remove `prune` and the empty-scan guard. The worker never deletes.
- Read scan roots from the SparkRoot settings file on each run (replaces `SCAN_ROOT`). Expand `~`.
- Write into `SparkRoot/Projects/` (replaces `TARGET_DIR`).
- Name each copy from front matter: camelCase(`project`) + `__` + camelCase(`project_type`) + `.md`. camelCase strips characters that aren't letters or digits and joins words (`Spark / Web App` → `sparkWebApp`, `side-project` → `sideProject`).
- Clash: same target name twice in one run → copy the first, log a warning naming both source paths, skip the rest.
- A `spark.md` with unreadable front matter: log and skip.
- Keep atomic writes (temp file + rename).
- Done when: a run against two test projects produces two correctly named files, a forced clash logs a warning, and nothing is ever deleted.

### 2. Web app: read the new layout (about 45 min)
Files: `web/projects.go`, `web/main.go`, `web/templates/*.html`, `web/static/style.css`.
- Read snapshots from `SparkRoot/Projects/` (`SPARK_DATA_DIR` default changes).
- Stop parsing machine and folder out of the filename; the filename is only the URL id. Name and type come from front matter.
- Remove `SPARK_MERGE`, `Machine`, `Folder`, `ShowMachine` and the machine label in both templates.
- Validate `project_type` against the project-types file in SparkRoot instead of `validType`.
- Done when: the dashboard and project pages render from new-style files, with no machine labels.

### 3. Settings storage (about 30 min)
- One small module in the web app to read and write the settings files in SparkRoot, atomically (temp + rename).
- First run: if a settings file is missing, create it with defaults (today's four project types).
- Done when: the web app starts with an empty SparkRoot and creates the default files.

### 4. Settings page (about 2 hours; the remote section depends on open question 1)
Files: new route and template in `web/`; follow `.mex/patterns/add-web-page.md`.
- Scan roots: list, add, remove.
- Project types: list, add, remove (plus color if open question 5 says so).
- Remote servers: add, remove, store the API key. Never echo a stored key back into the page.
- POST forms need CSRF protection, since the page now changes state.
- Behind `require` like every other page, so login applies when it is on.
- Done when: a scan root added in the page is picked up on the worker's next run.

### 5. Skill and setup (about 30 min)
Files: `skill/SKILL.md`, `skill/format.md`, `skill/template.md`, setup script or `INSTALL.md`.
- `format.md` and `template.md`: project types are no longer a fixed list; point to the project-types file in SparkRoot. Change these together with step 2 (existing non-negotiable).
- `SKILL.md`: read allowed types from that file before choosing one.
- Setup: create SparkRoot and its subfolders, put the skill in `SparkRoot/skill/`, symlink `~/.claude/skills/spark`, install the worker and its timer, optionally set login.
- Done when: a fresh machine can go from nothing to a card on the dashboard by following setup.

### 6. Container and docs (about 45 min)
Files: `Dockerfile`, `docker/entrypoint.sh`, `docker-compose.example.yml`, `INSTALL.md`, `README.md`.
- Container runs only the web app: remove the collector build, the collector loop, `COLLECT_INTERVAL`, `SCAN_ROOT`, `MACHINE_ID` and `/sources`.
- Mount SparkRoot (for example `~/Documents/Spark:/spark`), with `user:` in the compose example.
- Rewrite `INSTALL.md` around the new shape. Remove or update the central-server section per open question 7.
- Done when: `docker compose up -d` with the example file serves the dashboard from SparkRoot.

### 7. MEX scaffold (about 20 min)
- `.mex/AGENTS.md` non-negotiables: replace "the web app only reads", "never prune on an empty scan" and the machine-id filename scheme with the new rules.
- Update `context/architecture.md`, `context/setup.md`, `context/snapshot-format.md`, `ROUTER.md` current state.
- Mark the phase 2 decisions in `context/decisions.md` as Active (built).
- Update or retire `patterns/deploy-central-server.md` and `patterns/debug-missing-card.md`.

## Effects to keep in mind

- Renaming a project or changing its type leaves the old card until its old file is deleted by hand.
- The web app now writes (settings). Writes go only to the settings files, never to snapshots.
- Without login, anyone who can reach the site can change the settings and add remote servers. Keep the port on `127.0.0.1` unless login is on.
- Outside sources that write into SparkRoot should also write atomically, or the web app may briefly read a partial file. It already skips invalid files without crashing.

## Verify (each step)

`go vet ./...` and `gofmt -l .` in each module, plus the step's "Done when" check. There is
no test suite yet.
