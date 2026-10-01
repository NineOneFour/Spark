---
name: stack
description: Technology stack, library choices, and the reasoning behind them. Load when working with specific technologies or making decisions about libraries and tools.
triggers:
  - "library"
  - "package"
  - "dependency"
  - "which tool"
  - "technology"
edges:
  - target: context/decisions.md
    condition: when the reasoning behind a tech choice is needed
  - target: context/conventions.md
    condition: when understanding how to use a technology in this codebase
  - target: context/setup.md
    condition: when installing the toolchain or building binaries
# Broad inventory: ground only claims embodied by a small number of symbols.
grounds_to: []
last_updated: 2026-10-01
---

# Stack

## Core Technologies
- **Go 1.23**: the web app; `CGO_ENABLED=0` static build in Docker. Cross-compile with `GOOS`/`GOARCH` (for example, Raspberry Pi arm64).
- **Python 3, standard library only**: the collector (`collector/collector.py`), run on the host. No pip, no PyYAML.
- **Go standard library `net/http`** — routing via Go 1.22+ method/wildcard patterns (`GET /p/{id}`, `r.PathValue`). No router framework.
- **`html/template` + `embed`** — server-rendered pages; templates and static files compiled into the binary.
- **Plain CSS** (`web/static/style.css`) — no build step, no JS framework.
- **Markdown skill files** — the agent-side generator; no code.
- **Docker (alpine 3.20)**: the one image that holds everything; **cron** on the host schedules the collector.
- **POSIX sh**: `docker/entrypoint.sh` and `setup.sh`.

## Key Libraries
- **github.com/yuin/goldmark** (web) — Markdown rendering; relied on for default raw-HTML escaping.
- **gopkg.in/yaml.v3** (web) — front matter parsing into the `frontMatter` struct.

## What We Deliberately Do NOT Use
- No database or ORM: Markdown files in `SparkRoot/Projects/` and JSON in `Config/` are the store.
- No frontend framework, JS bundler, or JavaScript at all: pages are server-rendered HTML forms plus CSS.
- No HTTP router or config library: stdlib mux and a small hand-written env-file reader.
- No session store: auth is a stateless HMAC cookie.

## Version Constraints
- Go 1.22+ is required for the `http.ServeMux` patterns; `go.mod` pins `go 1.23`, which matches the Docker `golang:1.23-alpine` build image.
- `http.FileServerFS` (used for static) requires Go 1.22+.
