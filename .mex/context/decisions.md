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

<!-- Phase 4 decisions (2026-10-01). Designed, not built; full list in phase4-plan.md. -->

### Phase 4: Spark Handoff is a separate skill, run with or without the outgoing owner
**Date:** 2026-10-01
**Status:** Active (designed, not built)
**Decision:** A second skill, Spark Handoff ("Spark, handoff"), for passing a project completely to someone else. The outgoing owner runs it when they can; since they may be gone or unwilling (fired, walked out), anyone can run it, and it asks at the start whether the previous owner is there to help. Without them there is no interview: Traps and What's next are filled as best it can from the repo and marked as not confirmed by the previous owner. Commit messages count as the repo, used for any clues they hold (unlike `spark.md`, which never uses Git). It reads the repo (and `spark.md`, never writing it), drafts `handoff.md` next to `spark.md` in its own format, and asks the owner to correct and add to Traps and What's next. Sections: What it is, How to run it, How it's built, Current state, Key decisions (from the repo), Traps, What's next (from the owner). A rerun rewrites the repo sections and keeps the owner's earlier answers as the draft. The file stays in the repo; the dashboard doesn't read it. It ships in `SparkRoot/HandoffSkill/`, linked by `setup.sh` as `~/.claude/skills/spark-handoff`.
**Reasoning:** The user: Spark jogs the memory of someone who knows the project; a handoff must bring someone from zero, and it may be used when the owner left on bad terms. What's worth capturing is what leaves with the outgoing owner, and short questions about a concrete draft get better answers than blank ones.
**Alternatives considered:** The incoming owner runs it, or both in two steps; extra sections in `spark.md`; a `handoff/` folder; showing or pushing it on the dashboard; a People and access section; asking cold or one question at a time; rewriting from scratch or refusing on rerun; `Skills/` holding both skills; refreshing `spark.md` too.
**Consequences:** `spark.md`, `skill/format.md`, the collector, web app and sync API are unchanged. `handoff-skill/format.md` becomes the contract for `handoff.md`.

<!-- Phase 3 review decisions (2026-10-01). Built in commit bcb310a. -->

### Phase 3 review: a removed account's files retire to deleted-<name>
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** Removing an account on a remote renames its files and `state.json` entries from `username__…` to `deleted-username__…` (`deleted-username-2__…` after an earlier removal), and archives them automatically 30 days after removal. `deleted-` is reserved: no invite or `SPARK_USERNAME` may start with it. A hand archive or unarchive before then cancels the automatic one. Accounts removed before this change keep their old names.
**Reasoning:** The user: the files must not be lost, but a re-invited username must not inherit the old person's files and priorities.
**Alternatives considered:** Leaving files under the old username (re-invite inherits them); archiving at once; a bare `deleted` owner (two removed people with the same project collide); a dated name; anonymous `deleted-1`, `deleted-2`.
**Consequences:** The remote now renames snapshots, though it still never deletes them. A push writes its file under the state lock after rechecking its account, so it can't race a removal.

### Phase 3 review: "Push everything again" mirrors local to a remote
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** Each remote in local Settings → Remotes has a "Push everything again" button. It re-sends every file ever sent to that remote, plus new ones, with content, priority and archive flag, and local wins, overwriting priorities set on the remote. A file archived before its first push still stays local. A remote that was wiped is not detected automatically.
**Reasoning:** The user: a server going down and coming back empty is an edge case not worth designing for; a manual force sync that mirrors local is enough, and overwriting priorities is expected.
**Alternatives considered:** Detecting missing files from the priorities list on each pull; pulling priorities before the forced push.
**Consequences:** Removing a remote, or adding one under a removed name, forgets what was pushed to it (a different server may answer to that name). The forced push keeps the records and marks them changed.

### Phase 3 review: form tokens per account
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** With login on, the CSRF form token is an HMAC of the username and password tag under the session key. With login off it stays one random token per process.
**Reasoning:** One token per process meant every member of a remote saw the admin's token. The Sec-Fetch-Site/Origin checks and SameSite=Lax cookies already blocked cross-site posts; this removes the reliance on them.
**Alternatives considered:** A random token stored in each session.
**Consequences:** Logged-in forms no longer expire on restart; a password change replaces the token.

### Phase 3 review: a pushed id must match its snapshot
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** The remote rejects (422) a push whose id isn't `camelCase(project)__camelCase(project_type)` from the snapshot's own front matter. The collector's front matter reader matches YAML on quotes and `# comments`, so both sides compute the same name.
**Reasoning:** The id picks the card a file joins, so without the check one person could put a file of any project or type on another's card and set its name and description for everyone.
**Alternatives considered:** Deriving the id on the remote and ignoring the one sent.
**Consequences:** A hand-named file in local `Projects/` is never accepted by a remote; the local log shows the 422.

### Phase 3 review: end-to-end tests on demand, two harnesses
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** `web/e2e/` (build tag `e2e`) runs the same scenarios against Sparks as processes (`web/e2e/run.sh`) or as containers from the image (`-docker`). They run only when the user asks. Sync is triggered by real actions (a remote or state change pushes at once, a restart pulls at once), not by a test-only interval setting.
**Reasoning:** The user wanted both kinds, on demand. Real triggers keep test-only settings out of the product.
**Alternatives considered:** A browser-driven suite; a `SPARK_SYNC_SECONDS` setting; running in `go test ./...`.
**Consequences:** `go test ./...` stays unit-only. There is no CI yet.

<!-- Phase 3 decisions (2026-10-01). Built 2026-10-01; full list in phase3-plan.md. -->

### Phase 3: open items answered
**Date:** 2026-10-01
**Status:** Active
**Decision:** State is one `Config/state.json` keyed by file id. Last write wins by the remote's clock (stamped on arrival); a pull never overwrites an unpushed local change. Local finds content changes by polling every 60s and comparing hashes. The skill can still start a file archived (seeds archived at priority 5). Local login stays optional; the env account seeds the first account. Passwords are bcrypt; API keys are one per local deployment, shown once, stored as SHA-256 (amends "one account = one API key"); invites likewise, 7-day expiry. Unarchive from Settings → Archived.
**Reasoning:** The user picked each from options: remote clock avoids clock skew; polling works on Docker Desktop bind mounts; per-machine keys let a lost machine be revoked alone and mean no plaintext keys on the remote.
**Alternatives considered:** Config vs. State/ folder vs. sidecar files; each side's own clock; fsnotify; web-only archiving; always-on local login; argon2id; one viewable key per user; skill re-run unarchives; hand-editing state.json.
**Consequences:** `accounts.json` holds a key list per account. An unarchive UI was needed because archived files are hidden everywhere else.

### Phase 3: team sharing through remote deployments
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** One image with a `local`/`remote` mode flag; remote-only code behind one boundary. Local pushes to one or more remotes, choosing remotes by project type; each remote accepts a set list of types or all. Uploaded files are `username__project__type`. Content and archive go local → remote only; priority syncs both ways, last write wins. The local container pushes only when content, priority or archive changes, and asks each remote for priorities every 15 minutes. Front end renders goldmark output through bluemonday on every display.
**Reasoning:** The user's phase 3 spec, reconciled with phase 2. The collector rewrites identical files every 15 minutes, so pushes must be change-based.
**Alternatives considered:** Remote's type list replacing local's (blocks one local, many remotes); per-project remote selection; sync in the collector; priority only returned on push.
**Consequences:** "No database, no API" becomes "no database; API only in remote mode". See phase3-plan.md "Phase 2 rules this changes".

### Phase 3: priority and archive leave spark.md
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** Priority and archive live in a state file the web app owns. The skill's priority only sets the starting value; `archived` stops being a priority value. Archive is a per-side display flag.
**Reasoning:** The collector rewrites each snapshot every run, so state inside the file would overwrite priorities set on remote.
**Alternatives considered:** Keeping them in front matter and letting the web app edit the file.
**Consequences:** `format.md`, `template.md` and `web/projects.go` change together. The web app writes the state file as well as `Config/*.json`.

### Phase 3: one account per person on remote, invited by magic link
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** A remote account is one login, one API key and one tagging username. The first admin is set when the container starts; the admin creates accounts by a one-time link they copy and send themselves (no email). Only an admin or the file's owner changes a file's priority or archive state; only the admin edits colours. Local uses the same login system with account and invite features hidden, and routes split into remote-only and local-only.
**Reasoning:** The user: self-contained, no external identity provider or SMTP; one login system rather than two.
**Alternatives considered:** Self sign-up; emailed links; keeping local's single-user env login separate; anyone editing any file's priority.
**Consequences:** `web/auth.go`'s stateless single-user cookie is replaced.

### Phase 3: one card per project__type, priority colour per viewer
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** A card is one `project__type`, styled as on local (type accent, priority colour). On remote, tabs hold each user's file. A viewer with a file on the card sees their own priority colour; others see the most urgent.
**Reasoning:** The user: same as local. Grouping by project alone would give local cards with two untagged files.
**Alternatives considered:** One card per project name with tabs labelled by user and type.
**Consequences:** None yet.

<!-- Phase 2 decisions (2026-10-01). Built 2026-10-01 (phase2-plan.md steps 1-7).
     Older entries below that they supersede are kept for history. -->

### Phase 3: remote servers are deferred
**Date:** 2026-10-01
**Status:** Superseded by "Phase 3: team sharing through remote deployments" (2026-10-01)
**Decision:** Phase 2 stays local only. Remote server connections (and `remote_servers.json`, API keys on the settings page) move to phase 3. Their first open question: push this SparkRoot's snapshots up, pull a team's down, or both, and over what protocol.
**Reasoning:** The user: stay local before adding that complexity.
**Alternatives considered:** Designing the remote protocol during phase 2.
**Consequences:** The "everything in SparkRoot, including API keys" decision has no keys to hold yet.

### Phase 2: the image holds everything; setup.sh does the host steps
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** One image, run with `docker run` and SparkRoot mounted, carries the web app, the collector, the skill, `setup.sh` and `INSTALL.md`. On every start the entrypoint overwrites `collector.py`, `setup.sh`, `INSTALL.md` and `Skill/` in SparkRoot; `Config/` and `Projects/` are never overwritten. `setup.sh`, run once on the host from SparkRoot, creates the skill symlink and the cron line, because the container may not touch host folders outside SparkRoot. Login is `-e SPARK_USERNAME/SPARK_PASSWORD`.
**Reasoning:** The user: someone runs the image and it holds everything needed, and it will be published to Docker Hub in a later phase. Overwriting on start means pulling a new image updates the skill and collector.
**Alternatives considered:** A setup script that also writes a compose file and starts the container; copying the shipped files only when missing (edits survive, but old skills linger after an upgrade).
**Consequences:** Local edits to `Skill/` or `collector.py` are lost on restart; customization goes through `Config/`. The image name in docs is a placeholder until published.

### Phase 2: colors are per-deployment settings
**Date:** 2026-10-01
**Status:** Active (built, phase 2 steps 2–3)
**Decision:** Project type colors and priority 1–5 colors are stored in `Config/project_types.json` and `Config/priority_colors.json`, one color per entry for both light and dark mode. The web app serves them as a generated `/colors.css`. Today's hardcoded colors are the defaults (side-project and just-for-fun moved to middle-ground values). Type names and colors are held to strict patterns because they are written into CSS.
**Reasoning:** The user: each deployment carries its own colors; do types and priorities together rather than one now and the other later. One color per entry keeps the file and settings page simple.
**Alternatives considered:** Types only; a light and dark color per entry.
**Consequences:** `style.css` no longer contains card colors. A type missing from `project_types.json` hides its snapshots.

### Phase 2: the collector is a Python script inside SparkRoot
**Date:** 2026-10-01
**Status:** Active (built, phase 2 step 1)
**Decision:** The Go collector is replaced by `collector/collector.py` (Python 3, standard library only), installed at `SparkRoot/collector.py`. SparkRoot is the folder the script sits in, so it needs no env file or path setting. It reads `Config/scan_roots.json` and writes `Projects/projectName__projectType.md`. The name "collector" stays. The SMB target and central-server route are dropped. SparkRoot subfolders are `Projects/`, `Skill/`, `Config/`; settings are JSON, one file per concern. Old `machine__folder.md` files are deleted, not migrated.
**Reasoning:** The user: a script is enough, no binary needed; and a collector living in SparkRoot has no reason to be told where SparkRoot is. Scheduling is left to the user (cron).
**Alternatives considered:** Keeping the Go binary; a `SPARK_ROOT` env var; renaming to "worker".
**Consequences:** No Go module in `collector/`. The Docker build and `collector/systemd/*` are stale until phase 2 steps 5 and 6.

### Phase 2: the container is the core, SparkRoot is its one folder
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** The Docker container is the core of Spark for teams. It owns one host folder, SparkRoot (for example `~/Documents/Spark`), mounted as a bind mount with a `user:` line so files stay owned by the host user. SparkRoot holds subfolders (working names: `Projects/` for snapshots, `skill/` for the skill, plus config). The container never accesses any other host folder. Outside sources may also read and write SparkRoot.
**Reasoning:** One folder gives later workflow pieces a single place to read from and feed into. A bind mount, not a named volume, so host-side tools can reach the files.
**Alternatives considered:** Mounting host source folders into the container (today's Docker route); rejected because the container should not touch the host beyond SparkRoot.
**Consequences:** Supersedes the container's collector loop and `/sources` mounts. Something in the container now writes (settings), so "the web app never writes" no longer holds as written.

### Phase 2: the worker runs on the host
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** The worker (today's collector) runs on the host, not in the container. It reads its scan roots from a settings file in SparkRoot, searches them for `spark.md`, and copies each one into SparkRoot. That is all it does.
**Reasoning:** The container must not reach host folders; SparkRoot is the only link between host and container.
**Alternatives considered:** Mounting `~` read-only into the container; rejected (see above).
**Consequences:** Scan roots are edited from the settings page and picked up on the worker's next run. The worker needs to know where SparkRoot is.

### Phase 2: no machine ID; files are named projectName__projectType.md
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** Remove `MACHINE_ID` entirely. The snapshot filename is built from the snapshot's own front matter: `projectName__projectType.md`, both parts camelCase (for example `Spark Web App` + `side-project` → `sparkWebApp__sideProject.md`). Two scanned projects that produce the same filename: copy the first, skip the rest, log a warning.
**Reasoning:** Machine ID added complexity with no benefit to anyone. Same name and same type twice is almost certainly a mistake worth surfacing.
**Alternatives considered:** Last one wins (silent loss); path in the name (long, unstable names).
**Consequences:** `SPARK_MERGE` and the machine label on cards go away. Front-matter values themselves are unchanged; camelCase applies only to the filename.

### Phase 2: no automatic deletion
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** The worker never deletes files. Removing a project from the dashboard means deleting its file from SparkRoot by hand.
**Reasoning:** Without machine ID the worker cannot tell its own copies from files written by outside sources. "For now" — may be revisited.
**Alternatives considered:** Keeping prune (delete copies not found this run).
**Consequences:** Renaming a project or changing its type leaves the old card until its old file is deleted.

### Phase 2: settings page; all persistent state in SparkRoot
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** The front end stays as it is, plus a settings page that manages scan roots (add/remove), project types, and remote server connections. Everything that must survive a container rebuild is stored in SparkRoot, including remote server API keys.
**Reasoning:** Keeps the "files, no database" approach; SparkRoot is already the persistent folder.
**Alternatives considered:** Secrets in a separate mount or a restricted subfolder. Deferred.
**Consequences:** Accepted risk: anything that can read SparkRoot can read the API keys. To be revisited later.

### Phase 2: project types are data
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
**Decision:** Project types are no longer fixed in `skill/format.md`. They are edited on the settings page, stored in SparkRoot, and read by both the skill and the web app's validation.
**Reasoning:** Teams need their own types.
**Alternatives considered:** None discussed.
**Consequences:** `format.md`, `template.md`, and `web/projects.go` must change together (existing non-negotiable).

### Phase 2: setup creates the skill symlink; login off by default
**Date:** 2026-10-01
**Status:** Active (built 2026-10-01)
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
**Consequences:** No caching or indexing layer; every page view rereads the directory. Adding persistence would break this contract. Phase 2 renamed the files (`projectName__projectType.md`) and added `Config/*.json` settings; the principle is unchanged.

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
**Status:** Superseded by "Phase 2: the worker runs on the host" (2026-10-01, built)
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
**Status:** Superseded by "Phase 2: no automatic deletion" (2026-10-01, built)
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
**Consequences:** No multi-user support. Sessions last 30 days and can't be revoked individually. Phase 2: off by default for local use; when on, it also guards the settings page.
