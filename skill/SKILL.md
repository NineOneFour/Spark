---
name: spark
description: Generate a fresh spark.md snapshot of the current project so it can be picked back up after days or months away. Use when the user says "Spark, go", asks to run Spark, or is pausing, back-burnering, archiving, or returning to a project.
---

# Spark

## Purpose

The Spark skill creates a concise, current snapshot of a software project for use by the Spark project-memory dashboard.

The skill is intended to be invoked explicitly when a project is being paused, moved to the back burner, archived, or otherwise reaching a point where it may not be actively worked on for some time.

It can also be run when returning to a project whose snapshot is missing or stale. The behavior is identical either way.

Its purpose is to capture enough context that returning to the project days, weeks, or months later requires very little effort.

The resulting `spark.md` is a snapshot of the project **right now**. It is not a changelog, development journal, task tracker, or permanent project documentation.

## Invocation

The skill should support simple explicit invocation such as:

```text
Spark, go.
```

When invoked, the skill should inspect the current project and generate a fresh `spark.md`.

The user should not normally need to explain the project's current state manually. The skill should make a reasonable effort to determine it from existing project context.

## Fresh Generation

Every invocation produces a new snapshot.

If `spark.md` already exists:

1. Do not extend it.
2. Do not append to it.
3. Do not treat it as authoritative project context.
4. Replace/delete the existing file.
5. Generate a completely new `spark.md` from the project's current sources.

This requirement exists specifically to prevent `spark.md` from accumulating historical information over time.

The file describes the present, not the project's history.

## Context Discovery

The skill should gather enough information to accurately summarize the project without performing an exhaustive repository audit.

### 1. Mex

If the project uses Mex, inspect Mex first.

Start with the project's Mex router/index and follow relevant references to understand the project.

Prioritize Mex information concerning:

- Project purpose
- Current development state
- Architecture
- Current plans
- Outstanding work
- Known blockers
- Outstanding decisions
- Recent meaningful development

Do not blindly read every file referenced by Mex.

Follow only the context necessary to produce an accurate Spark snapshot.

### 2. Project Documentation

Review relevant project documentation when needed, including locations such as:

```text
docs/
notes/
README*
PROJECT-DESCRIPTION*
ROADMAP*
```

Also inspect other obviously relevant planning, design, or status documents discovered within the repository.

Do not assume every project follows the same documentation structure.

### 3. Codebase

Inspect the codebase when necessary to establish or verify the actual implementation state.

Code inspection should answer questions such as:

- Has a planned feature actually been implemented?
- Which major portions of the application currently exist?
- Does documentation appear inconsistent with the implementation?
- What major development phase does the project appear to have reached?

Generating `spark.md` is not a code review.

Do not perform a comprehensive audit merely to produce the snapshot.

## Source Priority

Prefer project knowledge in approximately this order:

```text
Mex
 ↓
Current project documentation
 ↓
Planning / notes
 ↓
Codebase verification
```

Use judgment when sources disagree.

Favor evidence representing the current state of the project over obviously stale planning material.

If a meaningful ambiguity cannot reasonably be resolved from the project, ask the user rather than inventing an answer.

## Output

Create `spark.md` in the project root.

The file must follow [format.md](format.md) exactly, using [template.md](template.md) as the skeleton: front matter fields, the controlled `project_type` values, section order, item structure, and length guidance all live there. Do not restate or reinterpret them here.

Skill-specific rules on top of the format:

- Write for someone returning after several months who needs to remember: *what is this, where did I leave it, and what do I need to think about next?*
- Several related implementation tasks should collapse into one high-level Remaining Work item.
- Do not ask for or write a priority. The user sets it in the web application.
- Read the allowed `project_type` names from `Config/project_types.json` in SparkRoot first. This skill folder is usually a symlink to `SparkRoot/Skill`, so resolve its real path (for example `realpath ~/.claude/skills/spark`) and read `../Config/project_types.json` from there. Infer one when the context makes it reasonably clear. If not, ask the user. Never invent a new value.

## Snapshot Date

Set `last_updated` to the current local time, with UTC offset, when the snapshot is generated (for example `2026-09-29T14:32:00-04:00`). Get it from the system clock, such as `date -Iseconds`, not from memory.

This value represents the last time the project's Spark state was intentionally reviewed and regenerated.

Do not derive this value from:

- Git
- Filesystem modification timestamps
- Existing `spark.md`
- Commit history

The Markdown value itself is authoritative.

## Git Independence

Spark does not require Git information.

Do not use commit SHAs or Git timestamps as Spark state.

Do not add Git metadata to `spark.md`.

Project state should be determined from Mex, documentation, planning material, and the current codebase.

## Length Check

After generating the file, count non-blank lines. If it exceeds the 200-line ceiling in [format.md](format.md), rewrite it more concisely and check again. Reduce detail rather than dropping project-state information.

## Guiding Principle

Spark should answer:

> What the hell was I doing with this project?

It should answer that question quickly.

When deciding whether information belongs in `spark.md`, prefer information that helps someone resume work over information that merely documents what happened.

Keep the snapshot current, concise, high-level, and disposable.

The next invocation of Spark will replace it completely.
