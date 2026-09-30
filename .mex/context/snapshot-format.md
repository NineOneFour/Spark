---
name: snapshot-format
description: The spark.md contract shared by the skill, the collector filename scheme, and the web parser. Load when changing front matter fields, sections, priorities, project types, or parsing/rendering.
triggers:
  - "spark.md"
  - "format"
  - "front matter"
  - "priority"
  - "project_type"
  - "section"
  - "parse"
edges:
  - target: context/architecture.md
    condition: when you need to see where parsing sits in the overall flow
  - target: patterns/change-snapshot-format.md
    condition: when actually adding or changing a field, value, or section
  - target: patterns/debug-missing-card.md
    condition: when a file fails validation and the card does not appear
  - target: context/decisions.md
    condition: when asking why the format is small and strict
# Graph indexed 0 files at setup (Go not indexed), so no grounding is possible yet.
grounds_to: []
last_updated: 2026-09-29
---

# Snapshot Format

The spec is `skill/format.md`; the skeleton is `skill/template.md`. The web app enforces it in `web/projects.go`. Those three must agree.

## Filename (collector → web)
- The collector writes `<MACHINE_ID>__<folder>.md`, where `<folder>` is the folder that holds `spark.md`.
- `parseFile` splits on the first `__`; a missing or empty half makes the file invalid. That is why `MACHINE_ID` must not contain `__`, `/`, or `\`.
- The filename without `.md` is the project `ID` used in `/p/{id}`.
- Dot-files and non-`.md` files are ignored, so the collector's `.<name>.tmp` files are never parsed.

## Front Matter (validated in `parseFile`)
- Must start with `---\n` and close with `\n---\n` (CRLF is normalized first).
- `project` and `description`: required, non-empty.
- `last_updated`: must parse as `time.RFC3339` (offset required). It decides which snapshot wins under `SPARK_MERGE`.
- `priority`: one of `1`–`5` or `archived` (`validPriority`). `archived` files are dropped from the dashboard.
- `project_type`: one of `key-project`, `side-project`, `experiment`, `just-for-fun` (`validType`).
- An invalid file is skipped and logged once per distinct error (`reportInvalid`), not on every request.

## Sections (`parseSections`)
- The body is split on top-level `# ` headings; empty sections are dropped.
- Titles in `listSections` (`Key Decisions Outstanding`, `Major Blockers`, `Remaining Work`) are parsed as `- Title` items, with an indented `- description` line under each (`parseItems`).
- All other sections render as Markdown prose through goldmark. Raw HTML is escaped by default, and that is load-bearing for safety.

## Presentation Coupling
- Cards get CSS classes `p-<priority>` and `t-<project_type>` (`web/templates/index.html`), defined in `web/static/style.css`. A new priority or type value needs matching CSS.
- The skill asks the user for priority using names (Right now … Shelved) mapped to the stored values in `format.md`, unless the invocation gives one (`Spark, go 5`, `Spark, go archived`).
