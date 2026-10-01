---
name: add-web-page
description: Add a new page or route to the Spark web app (template, handler, auth, CSP).
triggers:
  - "new page"
  - "add route"
  - "new view"
  - "handler"
edges:
  - target: context/architecture.md
    condition: to see the web server and auth components
  - target: context/conventions.md
    condition: for error handling and the verify checklist
grounds_to: []
last_updated: 2026-10-01
mex:
  id: mx_01M3QT591MQX0WQ8C6P6K8YGGY
  type: pattern
  status: promoted
  revision: 3
  title: add-web-page
  relations:
    - type: related_to
      target: mx_01M3QT58XQC6BMQHGY0BXGY4WY
      note: to see the web server and auth components
    - type: related_to
      target: mx_01M3QT58YT4Y543HS1636EZP7P
      note: for error handling and the verify checklist
---

# Add a Web Page

## Context
`web/main.go` parses one template set per page (`base.html` + `<page>.html`) into `s.tmpl` and registers routes on a stdlib `ServeMux` using Go 1.22 patterns. Everything passes through `securityHeaders`. Auth is `s.auth.require(handler)`, a no-op when login is off.

## Steps
1. Create `web/templates/<page>.html` defining `{{define "content"}}…{{end}}`; set `Title` in the data map for `base.html`.
2. Add `"<page>"` to the page list in `main()` so it gets parsed.
3. Write a `func (s *server) <page>(w, r)` handler; load data via `s.loadProjects()` if needed, then `s.render(w, r, "<page>", data)` (it adds `Viewer` and `Remote`). A page for one mode only is a method on `localMode` or `remoteMode`, registered in that mode's `routes`.
4. Register it: `mux.HandleFunc("GET /<path>", s.auth.require(s.<page>))` in `main()` (shared) or the mode's `routes`.
5. Add styles to `web/static/style.css`; rebuild (assets are embedded).

## Gotchas
- A template missing from the page list panics at startup (`template.Must`) or renders nothing (a nil map entry).
- CSP is `default-src 'self'`: no inline `<script>` or `style=""`, and no third-party hosts except Google Fonts. Update `securityHeaders` deliberately if needed.
- Use `GET /{$}`-style exact patterns; `GET /` alone matches everything.
- GET handlers are read-only. A POST that changes settings goes through `s.updateSettings(change)` (CSRF + `Sec-Fetch-Site`/`Origin` check, write lock, error re-render), and its form needs the hidden `csrf` field. Never write to `Projects/` (only the remote push API does).
- Never wrap user-controlled strings in `template.HTML`; only goldmark output is trusted.

## Verify
- [ ] Page renders with login off and, with login on, redirects to `/login` when there's no session
- [ ] Browser console shows no CSP violations
- [ ] `go vet ./...` clean in `web/`

## Debug
`render <page>: <err>` in the log means a template execution error. A 404 usually means a pattern mismatch.

## Update Scaffold
- [ ] Add the route to `.mex/context/architecture.md` Key Components if it's a significant feature
