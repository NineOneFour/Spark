---
name: conventions
description: How code is written in this project — naming, structure, patterns, and style. Load when writing new code or reviewing existing code.
triggers:
  - "convention"
  - "pattern"
  - "naming"
  - "style"
  - "how should I"
  - "what's the right way"
edges:
  - target: context/architecture.md
    condition: when a convention depends on understanding the system structure
  - target: context/stack.md
    condition: when deciding whether a new dependency is acceptable
  - target: patterns/add-config-setting.md
    condition: when adding or changing an environment setting
# Add only nodes that embody the documented convention; do not ground examples broadly.
# Graph indexed 0 files at setup (Go not indexed), so no grounding is possible yet.
grounds_to: []
last_updated: 2026-10-01
mex:
  id: mx_01M3QT58YT4Y543HS1636EZP7P
  type: convention
  status: promoted
  revision: 3
  title: conventions
  relations:
    - type: related_to
      target: mx_01M3QT58XQC6BMQHGY0BXGY4WY
      note: when a convention depends on understanding the system structure
    - type: related_to
      target: mx_01M3QT591CME72XWHMVWM94MNV
      note: when adding or changing an environment setting
---

# Conventions

<!-- mex:entity
id: mx_01M3QT58YKQZZA4E7HDZXG8FD0
type: convention
status: promoted
revision: 1
-->
## Naming
- Go files: short lowercase nouns per concern (`main.go`, `auth.go`, `projects.go`, `settings.go`, `settings_page.go`); the module is `package main`.
- Python: one script, `collector/collector.py`, snake_case functions, module docstring explaining the why.
- Env vars: web only, `SPARK_` prefix (`SPARK_ROOT`, `SPARK_ADDR`, …). The collector has none.
- SparkRoot subfolders are Title case (`Projects/`, `Skill/`, `Config/`); settings files are snake_case JSON (`scan_roots.json`).
- Unexported Go identifiers everywhere except the template-facing types `Project`, `Section`, `Item`, whose exported fields are read by `html/template`.
- Snapshot filenames: `projectName__projectType.md` (camelCase from front matter); temp files: `.<name>.tmp` or `.<name>.<random>.tmp`.
- CSS: `p-<priority>` / `t-<project_type>` modifier classes on `.card` and `.band`; their colors come from `/colors.css`.

<!-- mex:entity
id: mx_01M3QT58YCW8R8ZK2Q5EFGXZBB
type: convention
status: promoted
revision: 1
-->
## Structure
- `web/` is the only Go module. `collector/collector.py` is a standalone stdlib script; it shares no code with the web app. Duplicated logic (for example the scan-skip rules or `~` expansion) is deliberate.
- Web templates are `templates/base.html` plus one page file each, embedded with `//go:embed` and parsed per page in `web/main.go`. Every page defines `content` and renders through `base`.
- Static assets live in `web/static/` and are embedded, so a rebuild is required after CSS changes.
- Files shipped into SparkRoot (`collector/collector.py`, `setup.sh`, `INSTALL.md`, `skill/`) are copied by the `Dockerfile` into `/usr/local/share/spark/` and by `docker/entrypoint.sh` into SparkRoot on every start.
- User-facing docs live in `INSTALL.md` (also shipped) and `web/web.env.example`; `docs/` is gitignored.

<!-- mex:entity
id: mx_01M3QT58Y6MQ5ABYB90XZ3D4RT
type: convention
status: promoted
revision: 1
-->
## Patterns
Config: env file first, then env vars override, then defaults and validation, all in `loadConfig`:
```go
var configKeys = []string{"SPARK_ROOT", /* ... */}
for _, k := range configKeys {
    if v, ok := os.LookupEnv(k); ok { vals[k] = v }
}
```
A new key that is missing from `configKeys` silently ignores the env var.

Errors: `log.Fatalf` only at startup (config, settings). In request handlers, log the detail and send the user a generic message:
```go
log.Printf("load projects: %v", err)
http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
```
Settings form errors are a `userError`, shown on the page with a 400; anything else is logged and gets a generic 500 message.

The collector keeps going after a per-file failure, then exits 1 at the end. Bad front matter and name clashes are warnings, not failures.

Comments: a doc comment explains *why* (constraints, safety), for example "The web app can read at any moment, so write a temp file and rename it." Match that density.

<!-- mex:entity
id: mx_01M3QT58XZVXPHYKXTR002D5QY
type: convention
status: promoted
revision: 1
-->
## Verify Checklist
Before presenting any code:
- [ ] `go vet ./...` and `gofmt -l .` are clean in `web/`; `python3 -m py_compile collector/collector.py` passes
- [ ] If the `spark.md` contract changed: `skill/format.md`, `skill/template.md`, `web/projects.go`, and `collector/collector.py` (filename fields) all agree
- [ ] New env settings are in `configKeys`, `web/web.env.example`, the `INSTALL.md` table, and (if relevant) `Dockerfile` `ENV`
- [ ] The web app writes only `Config/*.json` (through `writeJSON`); the collector writes only `Projects/` (through `put`); nothing deletes snapshots
- [ ] Untrusted snapshot text still goes through goldmark or `html/template` escaping; no `template.HTML` built from raw input; anything written into `/colors.css` matches `typeNameRe`/`colorRe`
- [ ] New routes are wrapped in `s.auth.require(...)` unless deliberately public (login, static, colors.css); POST routes go through `updateSettings` (CSRF check)
- [ ] Anything that runs on the host stays inside SparkRoot except `setup.sh`'s symlink and cron line
- [ ] American English in code, comments, and docs
