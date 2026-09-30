---
name: debug-missing-card
description: Diagnose why a project's spark.md does not appear (or appears wrongly) on the dashboard, walking skill → collector → data dir → web parser.
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
last_updated: 2026-09-29
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
There are three boundaries, each with its own log: the collector (stdout / `journalctl --user -u spark-collector` / container logs), the data dir (plain files), and the web app (stdout). Work left to right.

## Steps
1. **Source:** does `<project>/spark.md` exist, and is it a regular file? It must sit in a folder under a `SCAN_ROOT`, not inside a dot-dir or `node_modules`/`vendor`/`build`/`dist`, and not under a parent folder that has its own `spark.md` (the scan stops descending there).
2. **Collector run:** look for `found N spark.md file(s)` and `copied <path> -> <name>`. Check for `skipping …: folder name clashes with …`: folder names that differ only by case collide, and the first path wins.
3. **Data dir:** confirm `<MACHINE_ID>__<folder>.md` exists in `TARGET_DIR` / `SPARK_DATA_DIR` / `/projects`. Make sure the collector and web app point at the same folder (each defaults to `projects/` next to its own binary).
4. **Web parse:** look for `skipping <file>: <reason>` in the web log. It is logged once per distinct error, so restart the web app to see it again.
5. **Filtering:** `priority: archived` hides the card. Under `SPARK_MERGE`, only the newest `last_updated` for that folder is shown.

## Gotchas
- Stale card that won't go away: pruning is skipped when a scan finds zero files, and it only removes files with this machine's `MACHINE_ID` prefix. A renamed `MACHINE_ID` leaves the old prefix's files forever; delete them by hand.
- `last_updated` without a UTC offset fails `time.RFC3339` parsing, so the card disappears.
- Docker: sources must be mounted under `/sources`, and the data dir must be writable by `--user`.
- SMB: `target: …` fatal errors are connection/auth/mount failures; check `SMB_HOST` port 445 reachability and credentials.

## Verify
- [ ] Card appears on `/` after a collector run and a page refresh
- [ ] Web log shows no `skipping` line for the file

## Debug
If all steps pass but the card is still missing, request `/p/<machine>__<folder>` directly. A 404 means the file isn't loaded; a render means the card grid template is at fault.

## Update Scaffold
- [ ] Add any newly discovered failure cause to Gotchas above and to `.mex/context/setup.md` Common Issues
