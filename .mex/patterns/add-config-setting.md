---
name: add-config-setting
description: Add a web app env setting or a SparkRoot/Config settings file, and document it everywhere users look.
triggers:
  - "add setting"
  - "env var"
  - "config option"
  - "environment variable"
  - "settings file"
edges:
  - target: context/setup.md
    condition: for the current list of settings and defaults
  - target: context/conventions.md
    condition: for the config-loading pattern and verify checklist
grounds_to: []
last_updated: 2026-10-02
mex:
  id: mx_01M3QT591CME72XWHMVWM94MNV
  type: pattern
  status: promoted
  revision: 3
  title: add-config-setting
  relations:
    - type: related_to
      target: mx_01M3QT5915NN48SVYHK9KSXFTW
      note: for the current list of settings and defaults
    - type: related_to
      target: mx_01M3QT58YT4Y543HS1636EZP7P
      note: for the config-loading pattern and verify checklist
---

# Add a Config Setting

## Context
There are two kinds of setting:
- **Process settings** (web app only): env vars with the `SPARK_` prefix, read in `loadConfig` in `web/main.go` as env file (`-config`) → env override for each name in `configKeys` → defaults and validation. The collector has none; it finds SparkRoot from its own location.
- **Deployment settings** (shared by web, collector, skill): JSON files in `SparkRoot/Config/`, one file per concern, defined in `web/settings.go`. The settings page edits them; users may also edit them by hand.

## Steps (process setting)
1. Add the key to `configKeys` in `web/main.go`.
2. Add a field to `config`, fill it from `vals[...]` in `loadConfig`, and set a default and validation there.
3. Document it in `web/web.env.example` (commented, with default) and the "Container setting" table in `INSTALL.md`.
4. If the container needs a default, add it to `ENV` in `Dockerfile`; show it commented in `docker-compose.example.yml` if users will set it.

## Steps (deployment setting)
1. Add a file-name constant and a default in `web/settings.go`, and add it to the `defaults` map in `ensureSettings` so a fresh SparkRoot gets it.
2. Write a loader that validates every entry and drops bad ones through `reportInvalid` (logged once), rather than failing the whole page.
3. If the page edits it: add a `change…` func in `web/settings_page.go`, register `POST /settings/<name>` wrapped in `s.auth.require(s.updateSettings(...))`, and add a form with the hidden `csrf` field to `templates/settings.html`.
4. Read-modify-write the file as written (not the validated view), so hand-written invalid entries are kept for the user to fix.
5. Document it in the Settings table in `INSTALL.md`. If the collector or skill reads it, update `collector/collector.py` or `skill/*.md` too.

## Gotchas
- Forgetting `configKeys`: the env file value works but the env var is silently ignored.
- The web app does not expand `~` in env values; the collector expands `~` in `scan_roots.json` on the host.
- Anything written into generated CSS (`/colors.css`) must be held to a strict pattern (`typeNameRe`, `colorRe`), not escaped.
- Paired settings (like username/password) should fail fast in `loadConfig`.
- A security limit needs a safe default and a startup warning when set looser: `warnLoosened` in `web/main.go` for process settings, the mode's constructor for mode-only ones (`newLocalMode`, `newRemoteMode`). Use `intSetting` for whole numbers.
- Add a setting with the step that uses it; an unused setting is dead config.

## Verify
- [ ] Process setting: works from the env file and from an env var, and the env var wins; default applies when unset
- [ ] Deployment setting: an empty SparkRoot gets the default file on start; a bad entry is logged once and skipped
- [ ] `go vet ./...` and `gofmt -l .` clean in `web/`
- [ ] `web.env.example` / `INSTALL.md` / `Dockerfile` updated as relevant

## Debug
Startup failures print `config: <error>` or `settings: <error>` and exit. A bad settings entry logs `skipping <file> entries: ...` once.

## Update Scaffold
- [ ] Add the setting to `.mex/context/setup.md`
