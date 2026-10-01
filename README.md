# Spark

A lightweight project-memory dashboard for answering one question when you come back to a project after days or months away:

> What the hell was I doing with this project?

Spark is not a task manager or issue tracker. It's a nicer-looking way to read a short snapshot of where you left off.

## How it works

1. **Run the skill.** In any project, tell your coding agent `Spark, go`. It reads the project's docs and code, asks you for a priority (or say `Spark, go 2` to set it up front), and writes a fresh `spark.md` to the project root. Run it before you step away, or when you come back.
2. **The collector syncs it.** A small Python script, run by cron, scans your source folders for `spark.md` files and copies them into SparkRoot as `projectName__projectType.md`.
3. **Open the dashboard.** The landing page shows every project as a card, color-coded by priority. Click one to read its snapshot, change its priority, or archive it. The settings page manages scan roots, remotes, project types and colors.

No database. The Markdown files are the source of truth; the only API is the sync between a local Spark and a team's remote one.

## Status

One Docker image holds everything. It owns one host folder, SparkRoot, and fills it with the skill, the collector, a host setup script and the settings. A team can run the same image in remote mode and push their snapshots to it (see INSTALL.md, "Sharing with a team").

## Install

See [INSTALL.md](INSTALL.md).

## Repo layout

```text
skill/
  SKILL.md      the Spark skill
  format.md     the spark.md format, shared by the skill and web app
  template.md   spark.md skeleton
collector/      collector.py: finds spark.md files and copies them into SparkRoot
web/            the dashboard and settings page
docker/         container entrypoint
setup.sh        host setup: skill symlink and cron line
```

## License

MIT
