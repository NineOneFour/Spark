---
name: add-config-setting
description: Add an environment/env-file setting to the collector or web app and document it everywhere users look.
triggers:
  - "add setting"
  - "env var"
  - "config option"
  - "environment variable"
edges:
  - target: context/setup.md
    condition: for the current list of settings and defaults
  - target: context/conventions.md
    condition: for the config-loading pattern and verify checklist
grounds_to: []
last_updated: 2026-09-29
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
Both binaries use the same shape in their own `main.go`: `readEnvFile` → override from env for each name in `configKeys` → build `config` struct → apply defaults and validation in `loadConfig`. The collector uses bare names; the web app uses the `SPARK_` prefix.

## Steps
1. Add the key to `configKeys` in `collector/main.go` or `web/main.go`.
2. Add a field to `config`, fill it from `vals[...]` in `loadConfig`, and set a default and validation there.
3. Pass it to where it's used (`server.cfg` in web; `cfg` in collector `main`).
4. Document it in the matching `collector/collector.env.example` or `web/web.env.example` (commented, with default).
5. Add a row to the Collector or Web app table in `INSTALL.md`.
6. If Docker needs a default, add it to `ENV` in `Dockerfile`; if the compose example should show it, add a commented line in `docker-compose.example.yml`.

## Gotchas
- Forgetting step 1: the env file value works but the env var is silently ignored.
- The collector treats a missing default env file as fine, but a missing file passed with `-config` is an error. The web app reads a file only when `-config` is given.
- Paths in the collector go through `expandHome`; the web app does not expand `~`.
- Paired settings (like username/password) should fail fast in `loadConfig`, as `SPARK_USERNAME`/`SPARK_PASSWORD` do.

## Verify
- [ ] Setting works from the env file and from an env var, and the env var wins
- [ ] Default applies when unset
- [ ] `go vet ./...` clean in the module
- [ ] env.example, INSTALL.md, and Dockerfile (if relevant) updated

## Debug
Startup failures print `config: <error>` and exit. Check validation messages in `loadConfig`.

## Update Scaffold
- [ ] Add the setting to `.mex/context/setup.md` Environment Variables
