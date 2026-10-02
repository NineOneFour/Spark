# Phase 4 Plan: Spark Handoff

Status: designed 2026-10-01 (10 locked decisions below), not built. It also carries the work left over
from the phase 3 review (commit `bcb310a`).
Decisions are also in `.mex/context/decisions.md`, under "Phase 4" and "Phase 3 review".

## What phase 4 is

From `phase3-plan.md`: "Phase 4 is the richer 'Spark handoff' (out of scope here)."

The idea (the user, 2026-10-01): a **separate skill, Spark Handoff**, for passing a project
completely to someone else. The Spark skill jogs the memory of someone who already knows the
project; Spark Handoff brings a person who knows nothing about it from zero to knowing what's
going on.

What phase 3 already gives handoff, from its front end spec: "Collaboration and handoff follow from
username tagging: each person's work on a project is a separate file; to hand off, the new person
starts their own copy and the previous owner archives theirs." Spark Handoff adds the document
that gets them up to speed; the card mechanics stay as they are.

## Locked decisions

1. **The outgoing owner runs Spark Handoff, when they can.** Like Spark it reads the repo, and it
   also interviews the person leaving for what isn't written down. Its value is capturing what
   would leave with them; the new owner reads the result. (Rejected: the incoming owner runs it,
   which only knows what's written down; both in two steps, two modes to build.)
   **Amended:** the previous owner may be gone or unwilling to help (people get fired and walk
   out), so anyone can run it. It asks at the start whether the previous owner is there to help.
   If not, there is no interview: it fills Traps and What's next as best it can from the repo.
2. **It writes `handoff.md` next to `spark.md`, in its own format**, as long as it needs to be.
   `spark.md` and its contract (`skill/format.md`, the length limit) don't change. (Rejected: extra
   sections in `spark.md`, which a plain Spark run would rewrite; a `handoff/` folder of docs,
   more to keep consistent.)
3. **`handoff.md` stays in the repo.** The new owner gets the repo, and the file is in it. No
   collector, web app or sync changes; the dashboard can learn about it later if it's missed.
   (Rejected: shown on the local project page; shown and pushed to remotes.)
4. **Sections.** Written from the repo: What it is, How to run it, How it's built, Current state,
   Key decisions. Drawn from interviewing the outgoing owner: Traps, What's next. (Rejected:
   People and access.)
5. **Draft first, then ask.** The skill reads the repo and drafts Traps and What's next from what
   it finds (TODOs, open issues, odd code), then asks the owner to correct and add to each.
   Without an owner (see 1), the draft is the result.
   (Rejected: open questions asked cold; a one-question-at-a-time conversation, longest to run.)
6. **Ships beside Spark's skill.** The image adds `SparkRoot/HandoffSkill/`; `setup.sh` adds a
   second link, `~/.claude/skills/spark-handoff`. `Skill/` doesn't move, so existing installs keep
   working and pick up the new skill by rerunning `setup.sh`. Trigger: "Spark, handoff" (assumed,
   to match "Spark, go"). (Rejected: `Skills/spark` and `Skills/spark-handoff`, which moves Spark's
   skill and needs every install to repoint its link.)
7. **A rerun rewrites, keeping the owner's answers.** The repo sections are rewritten fresh; the
   existing Traps and What's next become the draft the owner corrects, so nothing they said is
   lost unless they drop it. (Rejected: rewriting from scratch like `spark.md`; refusing and
   asking first.)
8. **Separate from `spark.md`.** Handoff reads `spark.md` if it's there (as input for Current
   state) but never writes it; run "Spark, go" for a fresh one. (Rejected: refreshing `spark.md`
   too, which makes Handoff depend on the Spark skill.)
9. **Unconfirmed sections are marked.** Without the previous owner, Traps and What's next say
   they were drafted from the repo and not confirmed by the previous owner, so the new owner can
   tell checked traps from inferred ones.
10. **Commit messages count as the repo.** Any clues in Git history (why something changed, what
    was tried) are used, for Key decisions above all. Unlike `spark.md`, which never uses Git.

## Steps (not built)

1. `handoff-skill/` in the repo, like `skill/`: `SKILL.md` (trigger "Spark, handoff"; read the
   repo, an existing `handoff.md` and `spark.md`; ask whether the previous owner is there to
   help; draft every section, using commit messages for clues; if the owner is there, ask them to
   correct and add to Traps and What's next, otherwise mark those two unconfirmed; write
   `handoff.md`), `format.md` (the contract for
   `handoff.md`: sections and their order), `template.md`.
2. `Dockerfile` copies it into the image; `docker/entrypoint.sh` copies it to
   `SparkRoot/HandoffSkill/` on every start, like `Skill/`.
3. `setup.sh` adds the `~/.claude/skills/spark-handoff` link, skipped when already done.
4. Docs: `INSTALL.md`, `README.md` repo layout. The e2e `TestImageFillsSparkRoot` checks
   `HandoffSkill/` too.
5. MEX scaffold.

## Carried over from phase 3

1. **Parse limits on pushed files.** The phase 3 "later hardening" item. Already in place: the id
   check (no path traversal), a 1 MB content cap, and yaml.v3's alias-expansion limits. Still open:
   the remote reparses every file on every page load, so many large pushes slow every page. First
   step: measure with a deliberately bad 1 MB push (about an hour), then add a limit or a parse
   cache only if needed.
2. **CI.** None yet. The unit tests (`go test ./...`) are cheap to run in CI. The end-to-end tests
   (`web/e2e/run.sh`, `-docker`) run only on demand.
3. **Unit tests.** Only `web/render_test.go`. Sync, parsing, filename patterns and auth are covered
   by the end-to-end tests only.

## Known limits, accepted for now

- A remote that is wiped is not detected; "Push everything again" in local Settings → Remotes
  mirrors local to it by hand.
- Accounts removed before commit `bcb310a` keep their files under the old username; only later
  removals rename to `deleted-<name>`.
- Publishing the image to Docker Hub is a later phase, not soon.
