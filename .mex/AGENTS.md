---
name: agents
description: Always-loaded project anchor. Read this first. Contains project identity, non-negotiables, commands, and pointer to ROUTER.md for full context.
last_updated: 2026-10-02
---

# Spark

## What This Is
A project-memory dashboard: an agent skill writes `spark.md` snapshots, a Python collector on the host copies them into one folder (SparkRoot), and a Go web app in a Docker container renders them as cards and manages settings. The same image runs as `local` (one person) or `remote` (a team server that locals push to).

## Non-Negotiables
- `skill/format.md` is the single contract for `spark.md`; change it, `skill/template.md`, and `web/projects.go` validation together
- `handoff-skill/format.md` is the contract for `handoff.md`, which only people read; the handoff skill never writes `spark.md`
- No database: Markdown files in `SparkRoot/Projects/` and JSON in `SparkRoot/Config/` are the only source of truth. The only API is the sync API, served in remote mode (`web/api.go`)
- The container never touches host folders outside SparkRoot; host-only steps belong in `setup.sh`
- The web app writes only `Config/*.json` (including `state.json`); on local it never writes, renames, or deletes snapshots. On remote, a push writes `Projects/username__project__type.md`, and removing an account renames its files to `deleted-username__…` (`retire`)
- Priority and archive live in `Config/state.json`; a snapshot's `priority` only seeds it
- Mode-only code lives in `web/local*.go` / `web/remote*.go` behind the `mode` interface in `main.go`; the shared core never checks the mode
- Nothing deletes snapshots automatically; all writes stay atomic (temp file + rename)
- Snapshot filenames are `projectName__projectType.md` from front matter; no machine ID anywhere
- Snapshot content is untrusted: keep goldmark's default HTML escaping, bluemonday on every render, and the CSP header

## Commands
One Go module (`web/`, Go 1.27+) and one Python script (`collector/collector.py`, stdlib); Go unit tests in `web/*_test.go`:
- Build web: `(cd web && go build -o web .)`
- Check: `go vet ./...`, `gofmt -l .` and `go test ./...` in `web/`; `python3 -m py_compile collector/collector.py`
- No local Go? Run them in `golang:1.27.1` with `docker run --rm -u $(id -u):$(id -g) -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -v "$PWD/web":/src -w /src golang:1.27.1 ...`
- End-to-end, only when the user asks: `web/e2e/run.sh` (processes) and `web/e2e/run.sh -docker` (image); see `patterns/run-e2e.md`
- Run locally: `SPARK_ROOT=<scratch> ./web/web`, copy `collector/collector.py` into that SparkRoot, run it
- Docker: `docker build -t spark .` then `docker run --user "$(id -u):$(id -g)" -v <SparkRoot>:/spark -p 127.0.0.1:8080:8080 spark`

## Code Graph
Use the smallest relevant structured resolver. For Inbox or Relay mutations, resolve only the intended action with `mex inbox contract --action <command-id> --json` or `mex relay contract --action <command-id> --json`; use `mex capabilities --json` only for broader capability discovery. If the user explicitly asks to create, save, or draft a checkout-local Inbox or Relay draft, preview and apply that exact draft without asking for redundant confirmation. Deleting a local draft, or publishing, approving, rejecting, withdrawing, marking stale, repairing, taking or acknowledging, or closing, requires fresh explicit confirmation after semantic preview. Treat Git commit, push, and pull as separate actions requiring their own authorization.

The repo is indexed into `.mex/graph.db`. Use it to avoid re-reading code you already have — it is one tool alongside Grep/Glob, not a replacement for them.
- **READ BROAD, GROUND TIGHT:** read the wide `scope` neighborhood to understand a task, but ground scaffold claims (`grounds_to`, `mex://` anchors) only to the few exact functions that embody them, using ids and fingerprints copied from graph output. Never invent ids or fingerprints.
- Known limit: at setup the graph indexed 0 files (Go and Python are not indexed), so scaffold files carry `grounds_to: []`. If `scope` returns `no-match`, fall back to Grep/Read. The code is small (~1,300 lines of Go, ~150 of Python).
- If you know the symbol name, go straight to it: `mex graph query <who-calls|what-calls|where-defined> <symbol>` and `mex graph get <id>` are exact and cheap. This is the strongest part of the graph. Give it exact names — an approximate name can return a confident wrong match.
- Exploring an unfamiliar task? `mex graph scope "<task>"` returns bounded, source-backed JSONL context plus trustworthy execution flows. Scope matches on words, not meaning, so treat it as starting evidence rather than a complete answer.
- Treat source returned by the graph as ALREADY READ; do not re-open those files.
- Read the summary status and evidence. `status: "ok"` remains usable when `truncated: true`; only optional evidence was omitted. For `partial` or `degraded`, narrow the task or follow `suggestedNextCommands`.
- Use `mex graph get <id> --detail source` only when source is missing, you need exact expansion, or a partial/degraded summary suggests it. Do not expand nodes by quota.
- If the evidence is insufficient or the task wording does not match the code, use Grep/Glob instead. Do not re-run `scope` with reworded phrasing more than once.
- Before editing a symbol, run `mex impact <symbol|file>` to see affected callers and scaffold memory.
- During `mex sync`, adjudicate any AMBIGUOUS grounding; after repairs, ensure the refreshed grounding is re-emitted.

## Scaffold Growth
After meaningful work, run GROW:
- Ground: what changed in reality?
- Record: update `ROUTER.md` and relevant `context/` files
- Orient: create or update a `patterns/` runbook if this can recur
- Write: bump `last_updated` on changed scaffold files; optional `mex log` notes follow the logging policy below

## Agent Logging
Read `mex logging --json` at session start and before optional logging. This checkout-local advisory preference is `significant` (quiet default: material decisions, risks, blockers, or durable discoveries), `checkpoints` (batch useful notes at task/session boundaries), or `manual` (no unsolicited notes). Skip routine tool calls, edits, repeated status, and empty summaries. Honor explicit user log requests in every mode; never suppress mandatory workflow Activity or recovery audit records. Report a policy read failure instead of guessing or changing the preference.

When earlier work may inform the task, use `mex timeline --query "subject phrase" --file src/example.ts --limit 10 --json` with a known subject or exact recorded file path, or both. These are historical notes, not accepted current knowledge. Verify conclusions before reuse or explicit promotion with their source retained.

The scaffold grows from real work, not just setup. See the GROW step in `ROUTER.md` for details.

## Navigation
At the start of every session, read `ROUTER.md` before doing anything else.
For full project context, patterns, and task guidance — everything is there.

<!-- mex-agent:skills:start -->
## MEX context policy
- When MEX context materially helps your work, mention MEX and the relevant finding naturally in your explanation. Tie the mention to what it helped you understand, decide, or verify. Avoid fixed phrases, standalone acknowledgements, repeated mentions, or narrating routine context loading. This replaces older MEX instructions requiring a fixed acknowledgement or context-loading narration.
- Do not claim an author, date, or historical event unless the retrieved data actually provides it.
- After a MEX write, say exactly what changed and its sharing boundary: a local draft is checkout-only and nothing is shared; a canonical artifact is written to the working tree and requires commit/push to share.
- Skill activation is not approval for canonical actions.
<!-- mex-agent:skills:end -->
