---
name: spark-handoff
description: Write a handoff.md that brings someone new up to speed on the current project so they can take it over. Use when the user says "Spark, handoff", or is handing a project to someone else or taking one over.
---

# Spark Handoff

## Purpose

The Spark Handoff skill writes `handoff.md`: everything a person who has never seen the project needs to take it over.

Spark (`spark.md`) jogs the memory of someone who already knows the project. Spark Handoff starts from zero. Its most valuable content is what the previous owner knows and the repo does not say, so it asks them when they are available.

## Invocation

```text
Spark, handoff.
```

Anyone can run it: the person leaving, the person taking over, or someone else.

## First: Is the Previous Owner Here?

Before anything else, ask in a plain-text message:

> Is the previous owner (the person handing this project off) here to answer questions?

- **Yes:** draft every section, then ask them to correct and add to Traps and What's next (see [Owner Review](#owner-review)).
- **No:** there is no interview. Draft every section from the repo; the draft of Traps and What's next is the result, with each new item tagged `(unconfirmed)`.

The previous owner may be gone or unwilling to help. Do not press for them.

## Context Discovery

Read enough of the project to write the file accurately. Go further than Spark does: the reader knows nothing, so How to run it and How it's built must be checked against the code, not just copied from docs.

Sources, in roughly this order:

1. **Mex**, if the project uses it: the router/index, then the relevant context files.
2. **Documentation and planning notes:** `README*`, `INSTALL*`, `docs/`, `notes/`, roadmaps, plans.
3. **`spark.md`**, if present: input for Current state. Check it against the repo; it may be stale.
4. **The code:** to confirm what exists, how it runs, and how the parts connect.
5. **Git history:** commit messages hold clues about why things changed and what was tried. Use them, for Key decisions above all. `git log` with full messages is a good start.
6. **An existing `handoff.md`**, if present (see [Rerun](#rerun)).

When drafting Traps and What's next, look for TODOs and FIXMEs, open issues, known-issue lists, workarounds, odd or defensive code, reverted commits, and planned work in notes.

If a meaningful ambiguity can't be resolved from the repo and the previous owner is not here, say so in the relevant section rather than inventing an answer.

## Owner Review

Only when the previous owner is here.

After drafting, show them the draft Traps, then the draft What's next, one section per message. For each, ask them to:

- correct or drop anything wrong,
- add anything missing (what would catch a new owner out; what they would do next).

Write their answers into the items. Items they confirm, correct, or add have no `(unconfirmed)` tag. Items they drop are removed.

They may also correct the repo sections; take their corrections.

## Rerun

If `handoff.md` already exists:

- Rewrite the repo sections (What it is, How to run it, How it's built, Current state, Key decisions) fresh.
- Use the existing Traps and What's next as the starting draft. Keep every existing item, tagged or not, unless the previous owner drops it. Add newly found items, tagged `(unconfirmed)`.
- With the previous owner here, Owner Review runs on that draft, so they can confirm older `(unconfirmed)` items.
- Without them, leave untagged items as they are. Unconfirmed items may be revised or removed if the repo no longer supports them.

Nothing the previous owner said is lost unless they drop it.

## Output

Write `handoff.md` in the project root, next to `spark.md`.

The file must follow [format.md](format.md), using [template.md](template.md) as the skeleton: the opening, section order, item structure, and the `(unconfirmed)` tag all live there.

Get the date for the status line from the system clock (`date +%F`).

Never create, change, or delete `spark.md`. To refresh it, the user runs "Spark, go".

## Guiding Principle

Write for someone who has never seen the project and can't ask the person who built it:

> What do I need to know to take this over?
