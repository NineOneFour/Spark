---
name: router
description: Session bootstrap and navigation hub. Read at the start of every session before any task. Contains project state, routing table, and behavioural contract.
edges:
  - target: context/architecture.md
    condition: when working on system design, integrations, or understanding how components connect
  - target: context/stack.md
    condition: when working with specific technologies, libraries, or making tech decisions
  - target: context/conventions.md
    condition: when writing new code, reviewing code, or unsure about project patterns
  - target: context/decisions.md
    condition: when making architectural choices or understanding why something is built a certain way
  - target: context/setup.md
    condition: when setting up the dev environment or running the project for the first time
  - target: context/snapshot-format.md
    condition: when working on spark.md fields, sections, validation, or rendering
  - target: patterns/INDEX.md
    condition: when starting a task — check the pattern index for a matching pattern file
last_updated: 2026-10-02
---

# Session Bootstrap

If you haven't already read `AGENTS.md`, read it now — it contains the project identity, non-negotiables, and commands.

Then read this file fully before doing anything else in this session.

## Current Project State

**Working:**
- Phase 3 is built (2026-10-01, `phase3-plan.md`): `SPARK_MODE=local|remote`. Local pushes snapshots to remotes by project type (60s hash poll, plus at once on priority/archive changes) and pulls priorities every 15 minutes. Remote: invite-only accounts, per-machine API keys, one card per `project__type` with a tab per user
- Priority and archive live in `Config/state.json`; the project page edits them, Settings → Archived unarchives
- Phase 2 (2026-10-01): one Docker image owns SparkRoot (`Projects/`, `Skill/`, `Config/`) and fills it on every start
- Phase 4 is built (2026-10-01): the Spark Handoff skill (`handoff-skill/`, "Spark, handoff") writes `handoff.md` for a new owner, per `handoff-skill/format.md`. Shipped to `SparkRoot/HandoffSkill/`, linked by `setup.sh` as `~/.claude/skills/spark-handoff`
- Skill (`skill/SKILL.md`) generates `spark.md` per `skill/format.md`; allowed types come from `Config/project_types.json`
- Collector: `collector.py` in SparkRoot, run by cron on the host; copies to `Projects/projectName__projectType.md`, never deletes
- Web app: card grid, per-project page, settings page (scan roots, remotes, project types, colors, archived), login (optional on local, required on remote)
- End-to-end tests (`web/e2e/`, `patterns/run-e2e.md`): real collector, local and remote, as processes or as containers from the image. Run only when asked
- `setup.sh` (host): skill symlink and cron line. `INSTALL.md` documents the Docker route plus a short no-Docker route

**Not yet built:**
- Phase 5 (`phase5-plan.md`): hardening against OWASP Top 10:2025 and the API Security Top 10; designed 2026-10-01 (18 locked decisions, 6 steps). Steps 1–2 built 2026-10-02 (Go 1.27.1/Alpine 3.24.2; `web/edge.go`: `SPARK_URL`, host check, `SPARK_ALLOW_NETWORK`, `SPARK_TRUSTED_PROXIES`, CSP/HSTS, bundled fonts). Step 3: login penalties and lockouts (`web/lockout.go`, `web unlock`), 15-character passwords, login form token, `security:` log lines. Step 4: server-side sessions (`web/sessions.go`, `Config/sessions.json`, 24h idle), real logout, Log out everywhere, change password; gorilla/sessions dropped. Step 5: API per-IP penalties, 120 calls/min per account, 128 KB files, 50 projects per account, limits in `/api/types` with local pacing, `http://` remotes private-only, key last use. Step 6 (tests and docs) not built. It covers the parse-limit leftover (file and project caps)
- Phase 3 leftovers (`phase4-plan.md`): CI (deferred past phase 5), unit tests
- Publishing the image to Docker Hub: a later phase, not soon. Until then, build it locally (`docker build -t spark .`); docs use the name `spark`
- CI, and unit tests beyond `web/render_test.go` (end-to-end tests exist: `web/e2e/run.sh`, on demand only)
- Code-graph coverage: `.mex/graph.db` indexed 0 files at setup (Go), so the scaffold has no `grounds_to` entries

**Known issues:**
- No automatic deletion (by design): renaming a project or changing its type leaves the old card until its file is deleted by hand
- Without login, anyone who can reach the port can change settings; keep it on `127.0.0.1` unless login is on
- Web rereads and reparses the whole data dir on every request (fine at current scale)

## Routing Table

Load the relevant file based on the current task. Always load `context/architecture.md` first if not already in context this session.

| Task type | Load |
|-----------|------|
| Understanding how the system works | `context/architecture.md` |
| Working with a specific technology | `context/stack.md` |
| Writing or reviewing code | `context/conventions.md` |
| Making a design decision | `context/decisions.md` |
| Setting up or running the project | `context/setup.md` |
| spark.md format, parsing, priorities, sections | `context/snapshot-format.md` |
| Any specific task | Check `patterns/INDEX.md` for a matching pattern |

## Behavioural Contract

For every task, follow this loop:

1. **CONTEXT** — Load the relevant context file(s) from the routing table above. Check `patterns/INDEX.md` for a matching pattern. If one exists, follow it.
2. **BUILD** — Do the work. If a pattern exists, follow its Steps. If you are about to deviate from an established pattern, say so before writing any code — state the deviation and why.
3. **VERIFY** — Load `context/conventions.md` and run the Verify Checklist item by item. State each item and whether the output passes. Do not summarise — enumerate explicitly.
4. **DEBUG** — If verification fails or something breaks, check `patterns/INDEX.md` for a debug pattern. Follow it. Fix the issue and re-run VERIFY.
5. **GROW** — After meaningful work, run this binary checklist:
   - **Ground:** What changed in reality? Name the changed behavior, system, command, dependency, or workflow.
   - **Record:** If project state changed, update the "Current Project State" section above. If documented facts changed, update the relevant `context/` file surgically.
   - **Orient:** If this task can recur and no pattern exists, create one in `patterns/` using `patterns/README.md`, then add it to `patterns/INDEX.md`. If a pattern exists but you learned a gotcha, update it.
   - **Write:** Bump `last_updated` in every scaffold file you changed. Read `mex logging --json` before optional `mex log` notes: `significant` records material rationale, `checkpoints` batches useful notes at task/session boundaries, and `manual` avoids unsolicited notes. Honor explicit user log requests in every mode; mandatory workflow Activity and recovery audits remain required.
