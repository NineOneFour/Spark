---
name: decisions
description: Key architectural and technical decisions with reasoning. Load when making design choices or understanding why something is built a certain way.
triggers:
  - "why do we"
  - "why is it"
  - "decision"
  - "alternative"
  - "we chose"
edges:
  - target: context/architecture.md
    condition: when a decision relates to system structure
  - target: context/stack.md
    condition: when a decision relates to technology choice
  - target: context/snapshot-format.md
    condition: when a decision concerns the spark.md contract
grounds_to: []
last_updated: 2026-10-01
---

# Decisions

<!-- HOW TO USE THIS FILE:
     Each decision follows the format below.
     When a decision changes: DO NOT delete the old entry.
     Mark it as superseded, add the new entry above it.
     The history must be preserved — this is the event clock. -->

## Decision Log

<!-- Phase 2 decisions (2026-10-01). Agreed in a design session; NOT YET BUILT.
     The code still follows the older entries below until phase2-plan.md is done. -->

### Phase 2: the container is the core, SparkRoot is its one folder
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** The Docker container is the core of Spark for teams. It owns one host folder, SparkRoot (for example `~/Documents/Spark`), mounted as a bind mount with a `user:` line so files stay owned by the host user. SparkRoot holds subfolders (working names: `Projects/` for snapshots, `skill/` for the skill, plus config). The container never accesses any other host folder. Outside sources may also read and write SparkRoot.
**Reasoning:** One folder gives later workflow pieces a single place to read from and feed into. A bind mount, not a named volume, so host-side tools can reach the files.
**Alternatives considered:** Mounting host source folders into the container (today's Docker route); rejected because the container should not touch the host beyond SparkRoot.
**Consequences:** Supersedes the container's collector loop and `/sources` mounts. Something in the container now writes (settings), so "the web app never writes" no longer holds as written.

### Phase 2: the worker runs on the host
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** The worker (today's collector) runs on the host, not in the container. It reads its scan roots from a settings file in SparkRoot, searches them for `spark.md`, and copies each one into SparkRoot. That is all it does.
**Reasoning:** The container must not reach host folders; SparkRoot is the only link between host and container.
**Alternatives considered:** Mounting `~` read-only into the container; rejected (see above).
**Consequences:** Scan roots are edited from the settings page and picked up on the worker's next run. The worker needs to know where SparkRoot is.

### Phase 2: no machine ID; files are named projectName__projectType.md
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** Remove `MACHINE_ID` entirely. The snapshot filename is built from the snapshot's own front matter: `projectName__projectType.md`, both parts camelCase (for example `Spark Web App` + `side-project` → `sparkWebApp__sideProject.md`). Two scanned projects that produce the same filename: copy the first, skip the rest, log a warning.
**Reasoning:** Machine ID added complexity with no benefit to anyone. Same name and same type twice is almost certainly a mistake worth surfacing.
**Alternatives considered:** Last one wins (silent loss); path in the name (long, unstable names).
**Consequences:** `SPARK_MERGE` and the machine label on cards go away. Front-matter values themselves are unchanged; camelCase applies only to the filename.

### Phase 2: no automatic deletion
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** The worker never deletes files. Removing a project from the dashboard means deleting its file from SparkRoot by hand.
**Reasoning:** Without machine ID the worker cannot tell its own copies from files written by outside sources. "For now" — may be revisited.
**Alternatives considered:** Keeping prune (delete copies not found this run).
**Consequences:** Renaming a project or changing its type leaves the old card until its old file is deleted.

### Phase 2: settings page; all persistent state in SparkRoot
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** The front end stays as it is, plus a settings page that manages scan roots (add/remove), project types, and remote server connections. Everything that must survive a container rebuild is stored in SparkRoot, including remote server API keys.
**Reasoning:** Keeps the "files, no database" approach; SparkRoot is already the persistent folder.
**Alternatives considered:** Secrets in a separate mount or a restricted subfolder. Deferred.
**Consequences:** Accepted risk: anything that can read SparkRoot can read the API keys. To be revisited later.

### Phase 2: project types are data
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** Project types are no longer fixed in `skill/format.md`. They are edited on the settings page, stored in SparkRoot, and read by both the skill and the web app's validation.
**Reasoning:** Teams need their own types.
**Alternatives considered:** None discussed.
**Consequences:** `format.md`, `template.md`, and `web/projects.go` must change together (existing non-negotiable).

### Phase 2: setup creates the skill symlink; login off by default
**Date:** 2026-10-01
**Status:** Accepted, not yet built
**Decision:** The skill lives in SparkRoot; setup symlinks `~/.claude/skills/spark` to it. Login stays off for local use and is turned on at setup when requested.
**Reasoning:** Local single-user use should need no login.
**Alternatives considered:** Requiring login for the settings page.
**Consequences:** A setup step or script is needed. Exposing the site beyond localhost without login exposes the settings page.

<!-- mex:entity
id: mx_01M3QT58ZVC2WNH99Z6V29NC4R
type: decision
status: promoted
revision: 1
-->
### Markdown files are the only source of truth
**Date:** 2026-09-29
**Status:** Active
**Decision:** No database or API; the web app reads `<machine>__<folder>.md` files from a directory on every request.
**Reasoning:** Stated in README ("No database, no API"). Snapshots are few and small, and files are easy to inspect, sync, and back up.
**Alternatives considered:** Not recorded in the repo.
**Consequences:** No caching or indexing layer; every page view rereads the directory. Adding persistence would break this contract.

<!-- mex:entity
id: mx_01M3QT58ZN7KW7H1V8YYH9FDND
type: decision
status: promoted
revision: 1
-->
### Snapshots are fresh and disposable, never appended
**Date:** 2026-09-29
**Status:** Active
**Decision:** The skill deletes and regenerates `spark.md` each run; `last_updated` comes from the system clock, never Git or file mtimes.
**Reasoning:** `skill/SKILL.md`: prevents the file from building up history; it describes the present only.
**Alternatives considered:** Not recorded in the repo.
**Consequences:** No changelog features. `last_updated` must include time and offset because it decides merge winners.

<!-- mex:entity
id: mx_01M3QT58ZECDV9K5NKTEPS5M8V
type: decision
status: promoted
revision: 1
-->
### Collector pushes files; separate writer and reader permissions
**Date:** 2026-09-29
**Status:** Superseded by "Phase 2: the worker runs on the host" (2026-10-01, takes effect when phase 2 is built)
**Decision:** Collectors upload over SMB (or copy locally) into the data dir. The web app runs as a separate account with read-only access.
**Reasoning:** `INSTALL.md`: "a compromised web app can't change snapshots". SMB stays LAN-only.
**Alternatives considered:** Not recorded in the repo.
**Consequences:** The web app must never gain a write path. Collector writes must be atomic because the reader can run at any moment.

<!-- mex:entity
id: mx_01M3QT58Z8J61Q5W60M6CQFMGY
type: decision
status: promoted
revision: 1
-->
### Never prune on an empty scan
**Date:** 2026-09-29
**Status:** Superseded by "Phase 2: no automatic deletion" (2026-10-01, takes effect when phase 2 is built)
**Decision:** The collector deletes this machine's stale files only when the current scan found at least one `spark.md`.
**Reasoning:** Comment in `collector/main.go`: an empty scan more likely means a wrong or missing root than that every project is gone.
**Alternatives considered:** Not recorded in the repo.
**Consequences:** Removing your last project leaves its card until another project exists or the file is deleted by hand.

<!-- mex:entity
id: mx_01M3QT58Z2FBVPF31HZSZV5A7K
type: decision
status: promoted
revision: 1
-->
### Optional single-user login with stateless cookies
**Date:** 2026-09-29
**Status:** Active
**Decision:** Login is off unless both `SPARK_USERNAME` and `SPARK_PASSWORD` are set. Sessions are HMAC-signed expiry cookies keyed from the credentials.
**Reasoning:** `web/auth.go`: no session store needed, and changing the password signs everyone out. Failed logins are serialized behind a 1-second delay to limit guessing.
**Alternatives considered:** Not recorded in the repo.
**Consequences:** No multi-user support. Sessions last 30 days and can't be revoked individually.
