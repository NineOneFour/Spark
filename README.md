# Spark

A lightweight project-memory dashboard for answering one question when you come back to a project after days or months away:

> What the hell was I doing with this project?

Spark is not a task manager or issue tracker. It's a nicer-looking way to read a short snapshot of where you left off.

## How it works

1. **Snapshot a project.** In any project, tell your coding agent `Spark, go`. It reads the project's docs and code and writes a fresh `spark.md` to the project root: what the project is, where you left it, and what's next. Run it before you step away, or when you come back.
2. **The collector picks it up.** A small Python script, run by cron every 15 minutes, scans your project folders for `spark.md` files and copies them into one folder, SparkRoot.
3. **Open the dashboard.** Every project is a card, color-coded by priority and type. Click one to read its snapshot, change its priority, or archive it. The settings page manages the folders to scan, project types, colors and remotes.
4. **Hand it off.** To pass a project to someone else, tell your agent `Spark, handoff`. It writes `handoff.md` next to `spark.md`: everything a new owner needs to take the project over. It asks the previous owner what the repo doesn't say, or, without them, marks what it couldn't confirm.

No database. The Markdown files are the source of truth.

## Teams

The same image runs in two modes. **Local** (the default) is your own Spark. **Remote** is a team server: each person's local Spark pushes their snapshots to it, and a project several people work on shows as one card with a tab per person. The remote has invite links, per-machine API keys, login penalties and rate limits, and is built to sit on the Internet behind HTTPS.

## Install

Needs Docker, `python3`, and a coding agent that loads skills from `~/.claude/skills` (such as Claude Code). From a checkout:

```sh
./setup.sh
```

It creates `~/Documents/Spark`, starts the `nineonefour/spark` container on http://localhost:9140, links the two skills, asks which folder holds your projects, and schedules the collector. Open the dashboard and your projects are there.

[INSTALL.md](INSTALL.md) has the rest: options, login, Compose, running a team remote, every setting, and updating.

## Repo layout

```text
skill/           the Spark skill ("Spark, go"), with the spark.md format and template
handoff-skill/   the Spark Handoff skill ("Spark, handoff"), with the handoff.md format and template
collector/       collector.py: finds spark.md files and copies them into SparkRoot
web/             the dashboard, settings page and team sync API (Go)
web/e2e/         end-to-end tests
docker/          container entrypoint
setup.sh         setup and upgrade: container, skill links, scan root and cron line
Dockerfile       the one image, for linux/amd64 and linux/arm64
```

## Development

Unit tests:

```sh
cd web && go test ./...
```

End-to-end tests run on demand, not in CI. They need only Docker and `python3`:

```sh
web/e2e/run.sh            # each Spark as a process
web/e2e/run.sh -docker    # each Spark as a container from the image
```

CI runs gofmt, go vet, the unit tests and govulncheck on every push. Pushing a `v1.2.3` tag publishes the image to Docker Hub.

## License

MIT
