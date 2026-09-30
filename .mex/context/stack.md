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
last_updated: 2026-09-29
---

# Stack

## Core Technologies
- **Go 1.23** — both binaries; `CGO_ENABLED=0` static builds in Docker. Cross-compile with `GOOS`/`GOARCH` (for example, Raspberry Pi arm64).
- **Go standard library `net/http`** — routing via Go 1.22+ method/wildcard patterns (`GET /p/{id}`, `r.PathValue`). No router framework.
- **`html/template` + `embed`** — server-rendered pages; templates and static files compiled into the binary.
- **Plain CSS** (`web/static/style.css`) — no build step, no JS framework.
- **Markdown skill files** — the agent-side generator; no code.
- **Docker (alpine 3.20)** and **systemd user units** — deployment/scheduling.

## Key Libraries
- **github.com/yuin/goldmark** (web) — Markdown rendering; relied on for default raw-HTML escaping.
- **gopkg.in/yaml.v3** (web) — front matter parsing into the `frontMatter` struct.
- **github.com/hirochachacha/go-smb2** (collector) — SMB2 client with NTLM for uploads to the central Samba share. Its `Rename` does not overwrite, which drives `smbTarget.Put`.

## What We Deliberately Do NOT Use
- No database or ORM: the data dir of Markdown files is the store.
- No frontend framework or JS bundler: pages are static HTML plus CSS.
- No HTTP router or config library: stdlib mux and a small hand-written env-file reader.
- No session store: auth is a stateless HMAC cookie.

## Version Constraints
- Go 1.22+ is required for the `http.ServeMux` patterns; `go.mod` pins `go 1.23`, which matches the Docker `golang:1.23-alpine` build image.
- `http.FileServerFS` (used for static) requires Go 1.22+.
