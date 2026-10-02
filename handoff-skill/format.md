# Spark Handoff Markdown Format

## Purpose

`handoff.md` brings someone who knows nothing about a project up to speed, so they can take it over.

This document defines its structure. The Spark Handoff skill writes the file; people read it. No program parses it.

`handoff.md` is separate from `spark.md`. Nothing in the Spark format applies here, and nothing here changes `spark.md`.

## File Location

```text
<project-root>/handoff.md
```

It sits next to `spark.md` and stays in the repo.

## Opening

No front matter. The file opens with a title and one status line:

```markdown
# Project Name Handoff

Written 2026-10-01. The previous owner took part.
```

or, when they did not:

```markdown
Written 2026-10-01. The previous owner did not take part; items marked (unconfirmed) were drafted from the repo.
```

The date is the day the file was last written, from the system clock. The status line describes the latest run only; the `(unconfirmed)` tags describe each item.

## Required Section Order

Sections are second-level headings, in this order:

```text
## What it is
## How to run it
## How it's built
## Current state
## Key decisions
## Traps
## What's next
```

All seven are always present. If a section has nothing to say, say so in one sentence (for example, "No traps found in the repo.") rather than leaving it out, so the reader knows it was checked.

## Sections Written from the Repo

What it is, How to run it, How it's built, Current state, and Key decisions are written from the repo: Mex, documentation, planning notes, code, `spark.md` if present, and Git commit messages.

They are rewritten fresh on every run.

### What it is

What the project is, who it is for, and the problem it solves. Prose.

### How to run it

What a new owner needs to build, run, test, and deploy it: prerequisites, commands, configuration, and where secrets or credentials are expected to come from (never the secrets themselves). Prose, lists, and code blocks as needed.

### How it's built

The main parts, how they fit together, and where each lives in the repo. Enough that the reader knows which file to open for a given change.

### Current state

What works, what is partly built, and what is missing. If `spark.md` exists, use it as input, but check it against the repo.

### Key decisions

Choices that shape the project and that a new owner might otherwise reverse by accident. Each uses:

```markdown
- Decision Title
	- What was chosen, why, and what was rejected.
```

Commit messages are a main source here.

## Sections from the Previous Owner

Traps and What's next hold what the previous owner knows that the repo may not say.

### Traps

Things that will catch a new owner out: fragile code, surprising behavior, manual steps, things that look wrong but are deliberate, things that look fine but are not.

### What's next

What the previous owner would do next, and in what order, including work that is planned but not written down.

### Items

Each item in Traps and What's next uses:

```markdown
- Item Title
	- 3-5 sentences.
```

The nested bullet is indented with one tab. For a trap, the description says what goes wrong and how to avoid or fix it. For a next step, it says what the work is and why it matters.

### The `(unconfirmed)` Tag

An item drafted from the repo and not confirmed by the previous owner ends its title line with ` (unconfirmed)`:

```markdown
- Cron runs the collector as root (unconfirmed)
	- Files it writes end up owned by root, and the web app can't read them. ...
```

An item with no tag was confirmed or written by the previous owner.

## Length

No limit. The file is as long as it needs to be to take someone from zero to working on the project. Prefer clear and complete over short.

## Design Principle

The file answers one question for someone who has never seen the project:

```text
What do I need to know to take this over?
```
