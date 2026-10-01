# Installing Spark

Spark has three pieces:

- **The skill** writes `spark.md` in a project when you say `Spark, go`.
- **The collector** (`collector.py`) finds `spark.md` files in your scan roots and copies them into SparkRoot.
- **The web app** shows them as cards, and has a settings page.

Everything ships in one Docker image. The container owns one host folder, **SparkRoot** (for example `~/Documents/Spark`), and fills it on start:

```text
SparkRoot/
  collector.py   the collector; run on the host by cron
  setup.sh       one-time host setup
  INSTALL.md     this file
  Skill/         the Spark skill
  Config/        settings (scan roots, project types, priority colors)
  Projects/      snapshots, named projectName__projectType.md
```

## 1. Start the container

Create SparkRoot first. If Docker creates it, root owns it and the container can't write to it.

```sh
mkdir -p ~/Documents/Spark
docker run -d --name spark \
  -p 127.0.0.1:8080:8080 \
  --user "$(id -u):$(id -g)" \
  -v ~/Documents/Spark:/spark \
  --restart unless-stopped \
  spark
```

- **`--user`:** keeps every file in SparkRoot owned by you instead of root.
- **`-p`:** keep `127.0.0.1:` unless login is on. Without login, anyone who can reach the page can change the settings.
- **Compose:** copy [`docker-compose.example.yml`](docker-compose.example.yml) to `docker-compose.yml`, set the path and `user:`, and run `docker compose up -d`.

The dashboard is at http://localhost:8080.

## 2. Set up the host

The container never touches host folders outside SparkRoot, so two steps run on the host. Needs `python3`.

```sh
~/Documents/Spark/setup.sh
```

It links `~/.claude/skills/spark` to `SparkRoot/Skill`, and adds a cron line that runs the collector every 15 minutes. The collector's last run is logged to `SparkRoot/collector.log`. Rerunning the script is safe.

## 3. Add scan roots

Open **Settings** on the dashboard and add the folders to scan, such as `~/Projects`. Each path must start with `/` or `~/`, and `~` means your home folder on the host. The collector picks them up on its next run. To run it right away:

```sh
python3 ~/Documents/Spark/collector.py
```

## 4. Use it

In any project, tell your coding agent `Spark, go`. The skill writes `spark.md`, the collector copies it into `Projects/`, and the card appears.

## Settings

All settings are files in `SparkRoot/Config/`, edited on the settings page or by hand:

| File | Holds |
|---|---|
| `scan_roots.json` | Folders the collector scans, for example `["~/Projects"]` |
| `project_types.json` | Allowed project types, each with a card color, for example `[{"name": "side-project", "color": "#64748b"}]` |
| `priority_colors.json` | Card color for each priority 1–5, for example `{"1": "#dc2626", ...}` |

The web app creates any missing file with its defaults on start. A snapshot whose `project_type` isn't listed is hidden.

| Container setting | Default | |
|---|---|---|
| `SPARK_USERNAME`, `SPARK_PASSWORD` | unset | Set both (`-e SPARK_USERNAME=me -e SPARK_PASSWORD=...`) to require login. Leave both unset for no login |
| `SPARK_ADDR` | `:8080` | Listen address inside the container |
| `SPARK_ROOT` | `/spark` | SparkRoot inside the container |

## Updating

Pull the new image and recreate the container. On every start it overwrites `collector.py`, `setup.sh`, `INSTALL.md` and `Skill/` with the image's copies, so don't edit those; customize through `Config/` instead. `Config/` and `Projects/` are never overwritten.

## Removing a project

Nothing is deleted automatically. Delete its file from `SparkRoot/Projects/` by hand. Renaming a project or changing its type leaves the old card until you delete the old file.

Two projects with the same name and type would share a filename: the collector copies the first, skips the rest, and logs a warning in `collector.log`.

## Without Docker

Build the web app (Go 1.23 or newer) and point it at SparkRoot:

```sh
(cd web && go build -o web .)
SPARK_ROOT=~/Documents/Spark ./web/web    # http://127.0.0.1:8080
```

Then copy `collector/collector.py`, `setup.sh` and `skill/` (as `Skill/`) into SparkRoot yourself, and run `setup.sh`. Without `SPARK_ROOT`, the web app uses the folder its binary sits in. `web/web.env.example` lists the settings; pass the file with `-config`.

## Moving from an older Spark

Older versions named snapshots `<machine-id>__<folder>.md` and used a systemd timer. Delete the old data folder, disable the old timer (`systemctl --user disable --now spark-collector.timer`), and let the collector refill `Projects/` from your `spark.md` files.
