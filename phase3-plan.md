# Phase 3 Plan: Team Sharing

Status: designed 2026-10-01; open items answered and built 2026-10-01 (steps below). The spec below came from a separate design chat; the
locked decisions after it came from reconciling that spec with phase 2. Where they differ, the
locked decisions win. They are also recorded in `.mex/context/decisions.md` under "Phase 3".
Phase 4 is the richer "Spark handoff" (out of scope here).

## Terminology

- **Local:** a Spark deployment on one user's machine (what phase 2 built).
- **Remote:** a hosted, multi-user Spark deployment that local deployments sync to.

## Spec

### Deployment
- One Docker image with a mode flag: `local` or `remote`.
- The front end is shared and identical in both modes.
- The shared core (front end, file handling, tagging) is mode-agnostic. Remote-only behavior
  sits behind one clear boundary that only runs in remote mode. No scattered "if remote" branches.

### Storage and tagging
- Remote stores raw markdown in one central folder. Each push overwrites the existing file.
- Local filenames stay `project__type`. On upload the username is added to the front:
  `username__project__type`.
- A remote accepts either a set list of project types or all types (locked 4), and exposes an
  endpoint that returns that list so local can fetch it.

### Sync contract

| State    | Direction      | Rule                                |
|----------|----------------|-------------------------------------|
| Content  | Local → Remote | One-way copy of latest state        |
| Archive  | Local → Remote | One-way; remote never pushes down   |
| Priority | Both ways      | Last write wins, regardless of side |

- Content sync never touches archive state.
- Example: a manager sets priority on remote, and it flows down to the owner's local.

### Archive
- Archive is a per-side display flag: archived means hidden from that side's front end, nothing more.
- Archiving locally also archives the file on remote. Archiving on remote does not affect local.
- Pushes to a file archived on remote still land, but the file stays hidden there.

### Authentication
- **Machine sync:** one API key per user per remote, even with several local deployments.
  Implemented as API key middleware.
- **Front-end login:** self-contained, no external identity provider. A light, explicit approach
  (gorilla/sessions plus custom handlers), not a framework like Authboss.

### Front end
- One card per `project__type` (locked 11). Clicking a card shows one tab per username, each
  holding that user's file.
- A file with no username (always the case on local) shows no tabs and renders plain.
- Archived files never appear on that side's front end.
- Collaboration and handoff follow from username tagging: each person's work on a project is a
  separate file; to hand off, the new person starts their own copy and the previous owner
  archives theirs.

### Security (day one, non-negotiable)
- **Threat model:** a leaked API key lets an attacker push markdown that renders in other
  users' browsers (stored XSS).
- **Pipeline:** render with goldmark, sanitize the HTML with bluemonday, then serve. Start from
  `UGCPolicy` and tighten it. This adds to goldmark's default HTML escaping and the CSP header;
  it does not replace them.
- **Where:** at display, on every render. Ingest stays a plain markdown write.
- **Code blocks** show as literal, inert text. Test that a code block holding raw HTML/script
  shows as readable code, doesn't execute, and doesn't show double-escaped entities.
- The remote never executes pushed content.

### Later hardening (not day one)
- Path traversal through crafted filenames or tags on ingest.
- Memory exhaustion or hangs when parsing pushed files.

## Locked decisions

1. Priority and archive move out of `spark.md` into a separate state file that the web app owns.
   The skill's priority only sets the starting value. (The collector rewrites every snapshot each
   run, so state inside the file would be overwritten.)
2. One remote account is one login, one API key and one username for tagging. A push is tagged
   with the username of the key that sent it.
3. One local can connect to many remotes. Local keeps and edits its own project types.
4. Each remote either accepts a set list of project types or accepts all.
5. Local sends projects to a remote by project type (for example, all `work` projects go to the
   work remote).
6. The admin creates accounts by magic link. The first admin is set when the container starts.
7. The admin copies the link and sends it themselves; the remote sends no email. The new user
   opens it, sets a password and gets their API key.
8. One login system in both modes. In local mode the account, invite and admin features are
   switched off (hidden, not removed). Routes are split cleanly into remote-only and local-only.
9. On remote, only an admin or the file's owner can change its priority or archive it.
10. The local container pushes on change only: content, priority or archive. The collector
    rewrites files with identical content every 15 minutes; those rewrites send nothing.
11. Local asks each remote for the priorities of its own files every 15 minutes.
12. One card per `project__type`, styled like local today: the type sets the accent, the
    priority sets the card colour.
13. On remote, only the admin edits the colour settings.
14. On a shared card, a viewer with a file there sees their own file's priority colour. Everyone
    else sees the most urgent priority among the card's files.

## Phase 2 rules this changes

These change when phase 3 is built, not before:

- "No database, no API" (`AGENTS.md`): remote gains a push endpoint, a project-types endpoint
  and a priority endpoint. Still no database.
- "The web app writes only `Config/*.json`": it also writes the state file (locked 1). It still
  never writes snapshots on local. On remote, pushes write snapshots.
- `skill/format.md`: `archived` stops being a priority value; priority in the file becomes the
  starting value only. `format.md`, `template.md` and `web/projects.go` change together.
- Login: today's single-user HMAC cookie (`web/auth.go`) is replaced by the shared login system
  (locked 8).

## Open items, answered (2026-10-01)

15. State lives in one `Config/state.json`: a JSON map keyed by file id (`project__type` on
    local, `username__project__type` on remote), written atomically under a mutex. An entry is
    `priority`, `priority_set`, `archived`; local entries also keep a `sync` record per remote.
    A file without an entry is seeded from its front matter the first time the web app sees it.
16. Last write wins by the remote's clock: the remote stamps `priority_set` when a change arrives.
    A local change waiting to be pushed is never overwritten by a pull.
17. The local container polls `Projects/` every 60 seconds and compares content hashes with what
    it last pushed to each remote. Priority and archive changes in the web app push at once.
18. The skill can still start a file as archived (`Spark, go archived`); like priority, it only
    seeds `state.json`. An archived seed starts at priority 5.
19. Local login stays optional and off by default. `SPARK_USERNAME`/`SPARK_PASSWORD` seed one
    account on start: the only user on local, the first admin on remote (required there).
20. Passwords are bcrypt. API keys are one per local deployment (amends locked 2: an account has
    many named keys, all tagging as its username), shown once, stored as SHA-256 hashes, revocable.
    Invite tokens are stored the same way and expire after 7 days.
21. Archived files are unarchived from an "Archived" list in Settings (on remote: your own files,
    or all files for the admin).

## Steps

1. `state.json` and seeding; `archived` becomes a starting value (`format.md`, `template.md`,
   `projects.go`); priority and archive controls on the project page; Archived list in Settings.
2. bluemonday after goldmark on every render, plus a test for code blocks holding HTML.
3. Shared login: accounts in `Config/accounts.json` (bcrypt), gorilla/sessions cookie keyed by
   `Config/session_key.json`, env seeds the first account. Replaces `web/auth.go`.
4. `SPARK_MODE=local|remote`. Remote-only code in `web/remote*.go`, local-only in `web/local*.go`;
   `main.go` picks one route set.
5. Remote: invites and accounts (admin), account page with API keys, accepted types
   (`Config/remote.json`), API (`GET /api/types`, `PUT /api/files/{id}`, `GET /api/priorities`)
   behind key middleware, cards grouped by `project__type` with a tab per user.
6. Local: remotes in `Config/remotes.json` (name, URL, key, types), managed in Settings; the
   sync loop (push on change, pull priorities every 15 minutes).
7. Docs and MEX scaffold.
