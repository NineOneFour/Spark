# Spark

A lightweight project-memory dashboard for answering one question when you come back to a project after days or months away:

> What the hell was I doing with this project?

Spark is not a task manager or issue tracker. It's a nicer-looking way to read a short snapshot of where you left off.

## How it works

1. **Run the skill.** In any project, tell your coding agent `Spark, go`. It reads the project's docs and code, asks you for a priority (or say `Spark, go 2` to set it up front), and writes a fresh `spark.md` to the project root. Run it before you step away, or when you come back.
2. **The collector syncs it.** The collector scans your source folders for `spark.md` files and copies them to Spark as `<machine-id>__<folder>.md`: into a local folder, or over SMB to a central server.
3. **Open the dashboard.** The landing page shows every project as a card, color-coded by priority. Click one to read its snapshot.

No database, no API. The Markdown files are the source of truth.

## Status

The skill, collector, and web app all work. Run them on one machine, in Docker, or as a central server with collectors on each machine.

## Install

See [INSTALL.md](INSTALL.md).

## Repo layout

```text
skill/
  SKILL.md      the Spark skill
  format.md     the spark.md format, shared by the skill and web app
  template.md   spark.md skeleton
collector/      finds spark.md files and copies them to Spark
web/            the dashboard
```

## License

MIT
