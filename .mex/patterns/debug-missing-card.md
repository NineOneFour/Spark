---
name: debug-missing-card
description: Diagnose why a project's spark.md does not appear (or appears wrongly) on the dashboard, walking scan roots → collector → SparkRoot → web parser.
triggers:
  - "card missing"
  - "not showing"
  - "project not on dashboard"
  - "collector not copying"
  - "stale card"
edges:
  - target: context/architecture.md
    condition: for the full flow and component boundaries
  - target: context/snapshot-format.md
    condition: when the web log says a file was skipped for a validation reason
  - target: patterns/change-snapshot-format.md
    condition: when the cause is a format mismatch that needs a code or spec change
grounds_to: []
last_updated: 2026-10-02
mex:
  id: mx_01M3QT592371G6KZQ0CGJ65X5K
  type: pattern
  status: promoted
  revision: 3
  title: debug-missing-card
  relations:
    - type: related_to
      target: mx_01M3QT58XQC6BMQHGY0BXGY4WY
      note: for the full flow and component boundaries
    - type: related_to
      target: mx_01M3QT591VSEBEKH63NBZKKRNE
      note: when the cause is a format mismatch that needs a code or spec change
---

# Debug a Missing Card

## Context
There are three boundaries, each with its own log: the collector on the host (`SparkRoot/collector.log`, last run only), SparkRoot itself (plain files), and the web app (container logs: `docker logs spark`). Work left to right.

## Steps
1. **Scan roots:** is the project's folder under a path in `SparkRoot/Config/scan_roots.json` (or the settings page)? `~` there means the host user's home.
2. **Source:** does `<project>/spark.md` exist as a regular file? It must not sit inside a dot-dir or `node_modules`/`vendor`/`build`/`dist`, nor under a parent folder that has its own `spark.md` (the scan stops descending there).
3. **Collector run:** is cron running it (`crontab -l`, or rerun `setup.sh`)? Run `python3 SparkRoot/collector.py` by hand. Look for `copied <path> -> <name>`, `skipping <path>: <reason>` (bad front matter), and `... is already taken by ...` (two projects with the same camelCased name and type; the first path wins).
4. **SparkRoot:** confirm `Projects/projectName__projectType.md` exists and the container mounts that same folder at `/spark`.
5. **Web parse:** look for `skipping <file>: <reason>` in the web log, most often `unsupported project_type` (the type is not in `project_types.json`). It is logged once per distinct error, so restart the container to see it again.
6. **Filtering:** an archived file is hidden: check `archived` in `Config/state.json` (or Settings → Archived). A snapshot that starts as `priority: archived` is archived the first time it's seen. On remote, the card shows only when at least one person's file on it isn't archived.

## Gotchas
- Stale card that won't go away: nothing is ever deleted automatically. Renaming a project or changing its type writes a new file and leaves the old one; delete it from `Projects/` by hand.
- Removing a type on the settings page hides every snapshot of that type.
- `last_updated` without a UTC offset fails `time.RFC3339` parsing, so the card disappears.
- Missing on a remote only: the remote rejects a push whose id isn't the collector's name for the snapshot (`camelCase(project)__camelCase(project_type)`), so a hand-named file in local `Projects/` never arrives. The local web log shows `push of <id> to <remote>: 422 ... does not match the snapshot`.
- A snapshot over `SPARK_MAX_FILE_KB` (128 KB) is skipped: the web log shows `skipping <file>: larger than 128 KB`, local doesn't push it, and a remote answers 413.
- Missing on a remote only, new project: over the account's project cap the push gets `409 ... already has N projects`; updates to existing ones still arrive. Raise it under Settings → Projects per person.
- Missing on a remote only, `http://` URL: local dials `http://` remotes only on private addresses (`... is not a private address`); use `https://` or `SPARK_ALLOW_HTTP_REMOTES=true`.
- The container refuses to start if `/spark` isn't writable: Docker created the host folder as root, or `--user` is missing.

## Verify
- [ ] Card appears on `/` after a collector run and a page refresh
- [ ] Web log shows no `skipping` line for the file

## Debug
If all steps pass but the card is still missing, request `/p/<filename without .md>` directly. A 404 means the file isn't loaded; a render means the card grid template is at fault.

## Update Scaffold
- [ ] Add any newly discovered failure cause to Gotchas above and to `.mex/context/setup.md` Common Issues
