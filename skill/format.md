# Spark Markdown Format

## Purpose

`spark.md` is the sole source of truth for a project's Spark state.

This document defines the exact structure that both the Spark skill and Spark web application must follow.

The skill generates `spark.md`.

The web application parses and renders `spark.md`.

The format should remain intentionally small, stable, and predictable.

## File Location

Each project stores its Spark snapshot at:

```text
<project-root>/spark.md
```

## Front Matter

Every `spark.md` file must begin with YAML front matter.

Required fields:

```yaml
---
project: Spark
description: Lightweight project-memory dashboard for remembering where development projects were left.
last_updated: 2026-09-29T14:32:00-04:00
priority: 1
project_type: side-project
---
```

### `project`

Human-readable project name.

Requirements:

- Required
- Plain text
- Should be concise
- Displayed on the landing-page card

### `description`

Short project description.

Requirements:

- Required
- One sentence maximum
- Intended for the landing-page card
- Should describe what the project is, not where it stands

### `last_updated`

Timestamp when the Spark snapshot was intentionally regenerated.

Format:

ISO 8601 with time and UTC offset:

```text
YYYY-MM-DDTHH:MM:SS±HH:MM
```

Requirements:

- Required
- Written by the Spark skill when generating the file
- Represents the age of the Spark snapshot
- Must not be derived from filesystem timestamps
- Must not be derived from Git timestamps

### `priority`

Required controlled value. Always chosen by the user, never inferred: given in the invocation (`Spark, go 5`) or asked for.

Supported values:

| Value      | Name          | Meaning                                                          |
|------------|---------------|------------------------------------------------------------------|
| `1`        | Right now     | What I'm working on                                              |
| `2`        | Up next       | Next in line                                                     |
| `3`        | When I can    | Will pick up when there's room                                   |
| `4`        | Eventually    | Not soon, but still intended                                     |
| `5`        | Someday maybe | Whenever, if ever                                                |
| `archived` | Shelved       | No work for the foreseeable future; starts archived (hidden)     |

The file stores the value. The name is only used when the skill asks the user; the web application shows priority as color.

The value is only the starting priority. The first time the web application sees a file, it copies the priority into `Config/state.json`; from then on priority and archiving are changed in the web application, and a new snapshot's value is ignored. (The collector rewrites every snapshot on each run, so state kept in the file would be lost.) `archived` starts the project archived, at priority 5.

`archived` also covers finished projects.

No other values are valid.

### `project_type`

Required controlled value.

Each deployment keeps its own list, with a color for each type, in `Config/project_types.json` in SparkRoot. The skill lives in `SparkRoot/Skill/` (reached through a symlink), so the file is `../Config/project_types.json` from the skill folder's real path:

```json
[
  {"name": "key-project", "color": "#7c3aed"},
  {"name": "side-project", "color": "#64748b"}
]
```

The value is a `name` from that file: lowercase letters and digits, words joined by hyphens. Only listed names are valid; the web application hides a snapshot whose type is not listed.

The defaults are `key-project`, `side-project`, `experiment` and `just-for-fun`.

## Required Section Order

Sections must appear in this order:

```text
Project Description
Current State
Last Major Push
Key Decisions Outstanding
Major Blockers
Remaining Work
```

The first three and `Remaining Work` are expected in normal project snapshots.

`Key Decisions Outstanding` and `Major Blockers` are optional.

If an optional section has no meaningful content, omit the section entirely.

Do not render placeholder content such as:

```text
None
N/A
No blockers
No decisions
```

## Project Description

Heading:

```markdown
# Project Description
```

Content:

- Approximately 3-5 sentences
- Explain what the project is
- Explain its purpose
- Explain the major problem or use case it addresses
- Avoid implementation trivia unless essential to understanding the project

## Current State

Heading:

```markdown
# Current State
```

Content:

- Approximately 3-5 sentences
- Describe where the project stands now
- Identify the current development phase or maturity
- Mention major functionality that exists or is still missing
- Include enough context to resume work without reopening the entire repository

This section describes the present state, not the history of the project.

## Last Major Push

Heading:

```markdown
# Last Major Push
```

Content:

- Approximately 3-5 sentences
- Summarize the most recent meaningful development effort
- Focus on the last coherent chunk of work
- Do not turn this into a changelog
- Do not enumerate individual commits or small fixes

## Key Decisions Outstanding

Heading:

```markdown
# Key Decisions Outstanding
```

This section is optional.

Include only significant decisions that still need to be made.

Do not include decisions that have already been settled.

Each decision must use:

```markdown
- Decision Title
	- One-sentence description of what still needs to be decided.
```

Guidelines:

- Maximum of approximately 3-4 items
- Keep each item high-level
- Omit the entire section when there are no meaningful outstanding decisions

## Major Blockers

Heading:

```markdown
# Major Blockers
```

This section is optional.

Include only conditions that prevent or materially constrain upcoming progress.

Each blocker must use:

```markdown
- Blocker Title
	- One-sentence description of why it blocks progress or what must happen to clear it.
```

Example:

```markdown
- Postgres 14 Is Sunset
	- The migration to a supported version must happen before deployment.
```

Guidelines:

- Maximum of approximately 3-4 items
- Do not treat every bug, incomplete feature, or TODO as a blocker
- Omit the entire section when there are no major blockers

## Remaining Work

Heading:

```markdown
# Remaining Work
```

This section contains only high-level remaining work.

Each item must use:

```markdown
- Task Title
	- One-sentence description.
```

Example:

```markdown
- Implement Authentication
	- Clerk was selected as the authentication provider; planning and implementation are still outstanding.
- Deploy to Proxmox
	- Spin up the production LXC, deploy the application, and configure routing and reverse proxy access.
```

Guidelines:

- Keep work grouped at a meaningful feature or phase level
- Do not include detailed implementation checklists
- Do not surface every issue, TODO, bug, or code-level task
- Prefer a small number of useful items over exhaustive completeness

## Length Limit

A valid `spark.md` must contain no more than:

```text
200 non-blank lines
```

This is a hard ceiling, not a target.

Most files should be substantially shorter.

If generated output exceeds the limit, reduce detail while preserving the most useful project-resumption context.

## Parsing Rules

The Spark web application should treat the file format as structured Markdown.

### Front Matter

The YAML front matter is authoritative for:

- project name
- short description
- snapshot timestamp
- priority
- project type

### Section Headings

Top-level headings define the project detail sections.

Expected top-level headings are:

```text
# Project Description
# Current State
# Last Major Push
# Key Decisions Outstanding
# Major Blockers
# Remaining Work
```

### Items

Within the following sections:

```text
Key Decisions Outstanding
Major Blockers
Remaining Work
```

each top-level bullet is one item, and its single nested bullet is the item's description:

```markdown
- Item Title
	- One-sentence description.
```

The nested bullet is indented with one tab.

## Invalid Files

A file should be considered invalid if required front-matter fields are missing or contain unsupported controlled values.

The application should not silently discard invalid files.

Invalid project files should produce a visible error condition in logs and should be surfaced in a safe diagnostic way rather than crashing the application.

The implementation details of that diagnostic behavior are left to the application.

## Source of Truth

The contents of `spark.md` are authoritative.

Do not supplement or override Spark state using:

- Git
- filesystem modification timestamps
- JSON sidecar files
- databases
- cached project metadata as an independent source of truth

Caching parsed data for performance is acceptable as long as it is derived from `spark.md` and can be rebuilt from the files.

## Design Principle

The format exists to answer three questions quickly:

```text
What is this project?
Where did I leave it?
What do I need to think about next?
```

Anything that does not help answer those questions probably does not belong in `spark.md`.
