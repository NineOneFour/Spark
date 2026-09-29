# Spark

A lightweight project-memory dashboard for answering one question when you come back to a project after days or months away:

> What the hell was I doing with this project?

Spark is not a task manager or issue tracker. It's a nicer-looking way to read a short snapshot of where you left off.

## How it works

1. **Run the skill.** In any project, tell your coding agent `Spark, go`. It reads the project's docs and code, asks you for a priority, and writes a fresh `spark.md` to the project root. Run it before you step away, or when you come back.
2. **The collector syncs it.** A collector on each machine scans your projects folder for `spark.md` files and copies them to the Spark server as `<machine-id>__<folder>.md`.
3. **Open the dashboard.** The landing page shows every project as a card, color-coded by priority. Click one to read its snapshot.

No database, no API. The Markdown files are the source of truth.

## Status

Design stage. The skill is usable now. The collector and web app are not built yet.

## Install the skill

```sh
git clone git@github.com:NineOneFour/Spark.git
ln -s "$PWD/Spark/skill" ~/.claude/skills/spark
```

## Repo layout

```text
skill/
  SKILL.md      the Spark skill
  format.md     the spark.md format, shared by the skill and web app
  template.md   spark.md skeleton
docs/
  project-description.md   architecture, landing page, deployment
  lifecycle-management.md  archiving and file flow
```

## License

MIT
