---
name: change-snapshot-format
description: Add or change a spark.md front matter field, controlled value (priority, project_type), or section, keeping the skill, spec, parser and collector in sync.
triggers:
  - "add field"
  - "new priority"
  - "new project type"
  - "new section"
  - "change format"
edges:
  - target: context/snapshot-format.md
    condition: always; it lists every place the contract is enforced
  - target: patterns/debug-missing-card.md
    condition: when existing snapshots stop appearing after the change
grounds_to: []
last_updated: 2026-10-01
mex:
  id: mx_01M3QT591VSEBEKH63NBZKKRNE
  type: pattern
  status: promoted
  revision: 2
  title: change-snapshot-format
  relations:
    - type: related_to
      target: mx_01M3QT592371G6KZQ0CGJ65X5K
      note: when existing snapshots stop appearing after the change
---

# Change the Snapshot Format

## Context
The contract lives in places that must agree: `skill/format.md` (spec), `skill/template.md` (skeleton), `web/projects.go` (validation + parsing), and `web/templates/*.html` (display). `collector/collector.py` reads only `project` and `project_type` (for the filename). `skill/SKILL.md` references `format.md` and should not restate it, except the priority-question wording and how to find `project_types.json`.

Project types and their colors are deployment data in `SparkRoot/Config/project_types.json`, not part of the format. Priority colors are in `priority_colors.json`. Adding a type is a settings change, not a format change.

## Steps
1. Edit `skill/format.md` first: field or value definition, section order, length guidance.
2. Update `skill/template.md` to match.
3. In `web/projects.go`:
   - new front matter field → add to `frontMatter` (yaml tag) and `Project`; validate in `parseFile`
   - new priority value → add to `validPriority`, and to `defaultPriorityColors` and the 1–5 loops in `web/main.go` (`colors`) and `web/settings_page.go` if it needs a color
   - new list-style section → add its exact title to `listSections`
4. Display: use the field in `index.html` / `project.html`.
5. If `project` or `project_type` change meaning, update `target_name` in `collector/collector.py`: they build the filename.
6. If the priority names changed, update the list in `skill/SKILL.md` ("Right now, Up next, …").
7. Rebuild the web binary (templates and CSS are embedded).

## Gotchas
- Stricter validation hides every existing snapshot that lacks the new field, because invalid files are skipped (logged once only). Prefer optional fields or regenerate all snapshots.
- The collector's front-matter reader is a flat `key: value` parser (stdlib only), not YAML. Nested or multi-line values won't parse there.
- `listSections` matches the heading text exactly, case included.
- `archived` only seeds `state.json` (`seedState`); `buildCards` hides archived files by their state, not their front matter.
- Section headings must be top-level `# `; `## ` lines are treated as body text.

## Verify
- [ ] `go vet ./...` clean in `web/`
- [ ] A sample snapshot in `SparkRoot/Projects/` using the new format renders on `/` and `/p/<id>`
- [ ] An old-format sample either still renders or its skip is logged with a clear message
- [ ] format.md, template.md and projects.go agree

## Debug
Run the web app and watch stdout for `skipping <file>: <reason>`. The reason string comes straight from `parseFile`.

## Update Scaffold
- [ ] Update `.mex/context/snapshot-format.md` with the new field/value/section
- [ ] Update `.mex/ROUTER.md` "Current Project State" if behavior changed
