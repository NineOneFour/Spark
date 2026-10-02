---
name: run-e2e
description: Run, extend, or debug Spark's on-demand end-to-end tests (processes or the Docker image).
triggers:
  - "e2e"
  - "end to end"
  - "end-to-end"
  - "run the tests"
edges:
  - target: context/architecture.md
    condition: to see how local, remote and the collector connect
  - target: context/conventions.md
    condition: for the verify checklist
grounds_to: []
last_updated: 2026-10-02
---

# Run the End-to-End Tests

## Context
`web/e2e/` holds Go tests behind the `e2e` build tag, so `go test ./...` never runs them. Run them only when the user asks. Each test starts its own Sparks on temp SparkRoots and free ports, runs the real `collector.py` with host `python3`, and drives them over HTTP: forms with form tokens, and the sync API with API keys. A Spark is either a web process (`-web <binary>`) or a container from an image (`-image <tag>`, on a per-run Docker network, so a local reaches a remote by container name).

Sync is triggered by real actions, not a test-only setting: adding or changing a remote, or a priority or archive change, pushes at once, and a restart pulls at once.

## Steps
1. Processes: `web/e2e/run.sh`. Image: `web/e2e/run.sh -docker` (builds `spark:e2e`, never `spark`). Run both when asked for "the e2e tests".
2. One test: append go test flags, e.g. `web/e2e/run.sh -test.run TestPushAndPull`.
3. To add a scenario: write a `Test…` in `web/e2e/scenarios_test.go` using `startSpark`/`startRemote`, `join` (invite + API key), `collect` (spark.md + collector run), `addRemote`, `newTeam`, and `waitFor` for anything done in the background. `send` makes one API push (no retry), `push` retries a 429, and `s.command(...)` runs a `web` subcommand such as `unlock` (the binary, or `docker exec` with the image). Pass limits as env to `startSpark`, e.g. `SPARK_PENALTY_START=0`, `SPARK_API_RATE=4`.

## Gotchas
- `run.sh` builds in `golang:1.27.1` and then runs the compiled test binary on the host, from `web/e2e/`, because the tests find `../../collector/collector.py` from there. The host needs Docker and `python3`; Go is not needed.
- Builds and the Go module cache live in `.e2e/` at the repo root (gitignored and dockerignored).
- Each test removes the processes and containers it started. After an interrupted run, check `docker ps -a --filter name=spark-e2e` and `docker network ls --filter name=spark-e2e`. Those are leftovers of the suite, but tell the user before removing them.
- A failing test prints the logs of every Spark it started.
- Passwords in tests must meet the 15-character minimum, and `login` goes through `submit` because the login form has its own token.
- Every client in a run comes from the same address, so wrong API keys (for example a local still pushing with a removed account's key) briefly make that address wait. Retry 429s, or give a test its own remote.

## Verify
- [ ] `web/e2e/run.sh` and `web/e2e/run.sh -docker` both end in `PASS`
- [ ] No `spark-e2e` containers or networks are left

## Update Scaffold
- [ ] A new scenario that guards a design rule: name it in `context/architecture.md` if the rule lives there
