---
project: Spark
description: Lightweight project-memory dashboard that shows where each development project was left off.
last_updated: 2026-09-29T20:47:18-04:00
priority: 3
project_type: side-project
---

# Project Description

Spark answers one question when returning to a project after days or months away: "What the hell was I doing with this project?" An agent skill (`Spark, go`) writes a fresh, disposable `spark.md` snapshot into a project's root. A Go collector finds those files and copies them into one data folder, locally or over SMB to a central server. A Go web app renders them as a card grid color-coded by priority, with a detail page per project. There is no database and no API: the Markdown files are the only source of truth.

# Current State

All three parts work end to end: the skill (`skill/SKILL.md`, `format.md`, `template.md`), the collector (scan, atomic copy, prune stale files, systemd user timer), and the web app (cards, project pages, optional login, `SPARK_MERGE` for same-named projects across machines). `INSTALL.md` documents three deployment routes: localhost, a single Docker container with a collector loop, and a central server with Samba and Caddy. The repo is initialized with MEX, which holds the architecture, decisions, and format context. There are no automated tests or CI yet, and the MEX code graph indexed zero Go files, so scaffold grounding is empty.

# Last Major Push

The most recent work made the tool usable beyond one machine. Deployment options were added (localhost, Docker, and a central-server pattern with per-machine collectors pushing over SMB), along with `INSTALL.md`. The skill gained inline priority (`Spark, go 3`) so it no longer has to ask. MEX was set up to capture the project's architecture and decisions.

# Remaining Work

- Add Tests and CI
	- Cover `web/projects.go` validation and the collector's scan/prune logic, then run `go vet`, `gofmt`, and tests in CI for both modules.
- Handle the Empty-Scan Edge Case
	- The collector never prunes on an empty scan by design, so the last removed project's card lingers; decide on a safe way to clear it.
- Revisit Per-Request Reparsing
	- The web app rereads the whole data directory on every request, which is fine now but may need a cache derived from the files if the project count grows.
