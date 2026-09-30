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
last_updated: 2026-09-29
---

# Decisions

<!-- HOW TO USE THIS FILE:
     Each decision follows the format below.
     When a decision changes: DO NOT delete the old entry.
     Mark it as superseded, add the new entry above it.
     The history must be preserved — this is the event clock. -->

## Decision Log

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
**Status:** Active
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
**Status:** Active
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
