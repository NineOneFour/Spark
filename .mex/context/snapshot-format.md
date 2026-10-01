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
last_updated: 2026-10-01
---

# Snapshot Format

The spec is `skill/format.md`; the skeleton is `skill/template.md`. The web app enforces it in `web/projects.go`. Those three must agree.

## Filename (collector → web)
- The collector writes `Projects/<camel(project)>__<camel(project_type)>.md`. `camel_case` joins the letter/digit runs (`Spark / Web App` → `sparkWebApp`, `side-project` → `sideProject`).
- Same target name twice in one run (case-insensitive): the first path (sorted) is copied, the rest are skipped with a warning.
- The filename without `.md` is the `ID` (the `state.json` key). `mode.fileKey` splits it into owner and card `Key`: on local the key is the whole id; on remote it is `username__` + the local id. Name and type come from front matter.
- Dot-files and non-`.md` files are ignored, so `.<name>.tmp` temp files are never parsed.

## Front Matter (validated in `parseFile`)
- Must start with `---\n` and close with `\n---\n` (CRLF is normalized first).
- `project` and `description`: required, non-empty.
- `last_updated`: must parse as `time.RFC3339` (offset required).
- `priority`: one of `1`–`5` or `archived` (`validPriority`). Only a starting value: `seedState` copies it into `Config/state.json` the first time the file is seen (`archived` → archived, priority 5); later snapshots' values are ignored.
- `project_type`: a `name` listed in `SparkRoot/Config/project_types.json` (reread per request in `loadProjects`). Defaults: `key-project`, `side-project`, `experiment`, `just-for-fun`. Names match `typeNameRe` (lowercase, hyphen-joined).
- An invalid file is skipped and logged once per distinct error (`reportInvalid`), not on every request.

## Sections (`parseSections`)
- The body is split on top-level `# ` headings; empty sections are dropped.
- Titles in `listSections` (`Key Decisions Outstanding`, `Major Blockers`, `Remaining Work`) are parsed as `- Title` items, with an indented `- description` line under each (`parseItems`).
- All other sections render as Markdown prose through goldmark. Raw HTML is escaped by default, and that is load-bearing for safety.

## Presentation Coupling
- Cards get CSS classes `p-<priority>` and `t-<project_type>` (`web/templates/index.html`, `project.html`). Their colors are not in `style.css`: `GET /colors.css` generates `--pc`/`--tc` from `priority_colors.json` and `project_types.json`. A new type needs only a settings entry.
- The skill asks the user for priority using names (Right now … Shelved) mapped to the stored values in `format.md`, unless the invocation gives one (`Spark, go 5`, `Spark, go archived`).
