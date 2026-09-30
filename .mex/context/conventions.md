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
last_updated: 2026-09-29
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
- Go files: short lowercase nouns per concern (`main.go`, `target.go`, `auth.go`, `projects.go`); both modules are `package main`.
- Env vars: collector uses bare names (`SCAN_ROOT`, `MACHINE_ID`, `TARGET_DIR`, `SMB_*`); web uses the `SPARK_` prefix (`SPARK_DATA_DIR`, `SPARK_ADDR`, …). Keep that split.
- Unexported identifiers everywhere except the template-facing types `Project`, `Section`, `Item`, whose exported fields are read by `html/template`.
- Snapshot filenames: `<machine-id>__<folder>.md`; temp files: `.<name>.tmp`.
- CSS: `p-<priority>` / `t-<project_type>` modifier classes on `.card`.

<!-- mex:entity
id: mx_01M3QT58YCW8R8ZK2Q5EFGXZBB
type: convention
status: promoted
revision: 1
-->
## Structure
- `collector/` and `web/` are separate Go modules with their own `go.mod`; they share no code. Duplicated helpers such as `readEnvFile` are deliberate. Don't create a shared module for them.
- Web templates are `templates/base.html` plus one page file each, embedded with `//go:embed` and parsed per page in `web/main.go`. Every page defines `content` and renders through `base`.
- Static assets live in `web/static/` and are embedded, so a rebuild is required after CSS changes.
- User-facing docs live in `INSTALL.md` (config tables, routes) and the `*.env.example` files; `docs/` is gitignored.

<!-- mex:entity
id: mx_01M3QT58Y6MQ5ABYB90XZ3D4RT
type: convention
status: promoted
revision: 1
-->
## Patterns
Config: env file first, then env vars override, then defaults and validation, all in `loadConfig`:
```go
var configKeys = []string{"SPARK_DATA_DIR", /* ... */}
for _, k := range configKeys {
    if v, ok := os.LookupEnv(k); ok { vals[k] = v }
}
```
A new key that is missing from `configKeys` silently ignores the env var.

Errors: `log.Fatalf` only at startup (config, target). In request handlers, log the detail and send the user a generic message:
```go
log.Printf("load projects: %v", err)
http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
```
The collector keeps going after a per-file failure, then exits 1 at the end.

Comments: a doc comment explains *why* (constraints, safety), for example "SMB rename will not overwrite, so the old file is removed first". Match that density.

<!-- mex:entity
id: mx_01M3QT58XZVXPHYKXTR002D5QY
type: convention
status: promoted
revision: 1
-->
## Verify Checklist
Before presenting any code:
- [ ] `go vet ./...` and `gofmt -l .` are clean in each touched module (`collector/`, `web/`)
- [ ] If the `spark.md` contract changed: `skill/format.md`, `skill/template.md`, `web/projects.go`, and CSS all agree
- [ ] New settings are in `configKeys`, the matching `*.env.example`, the `INSTALL.md` table, and (if relevant) `Dockerfile` `ENV`
- [ ] The web app still performs no writes to the data dir; collector writes still go through `target.Put`
- [ ] Untrusted snapshot text still goes through goldmark or `html/template` escaping; no `template.HTML` built from raw input
- [ ] New routes are wrapped in `s.auth.require(...)` unless they are deliberately public (login, static)
- [ ] American English in code, comments, and docs
