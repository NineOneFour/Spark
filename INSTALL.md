# Installing Spark

Spark has three pieces:

- **The skill** writes `spark.md` in a project when you say `Spark, go`. A second skill, Spark Handoff, writes `handoff.md` when you say `Spark, handoff`.
- **The collector** (`collector.py`) finds `spark.md` files in your scan roots and copies them into SparkRoot.
- **The web app** shows them as cards, and has a settings page.

Everything ships in one Docker image. The container owns one host folder, **SparkRoot** (for example `~/Documents/Spark`), and fills it on start:

```text
SparkRoot/
  collector.py   the collector; run on the host by cron
  setup.sh       setup and upgrade (a copy; the repo has the same script)
  INSTALL.md     this file
  Skill/         the Spark skill
  HandoffSkill/  the Spark Handoff skill
  Config/        settings, plus state.json (each project's priority and archive flag)
  Projects/      snapshots, named projectName__projectType.md
```

## 1. Set up

Needs Docker and `python3`. From a checkout of this repo:

```sh
./setup.sh
```

`setup.sh` does the whole setup:

1. Creates SparkRoot, `~/Documents/Spark`. If Docker created it, root would own it and the container couldn't write to it.
2. Pulls the image, [`nineonefour/spark`](https://hub.docker.com/r/nineonefour/spark), and starts the `spark` container on `127.0.0.1:9140`, running as you so every file in SparkRoot stays yours, and waits until the dashboard answers.
3. Links `~/.claude/skills/spark` to `SparkRoot/Skill` and `~/.claude/skills/spark-handoff` to `SparkRoot/HandoffSkill`.
4. Asks which folder holds your projects (default `~/Projects`, if you have one) and adds it as a scan root. It asks only while no scan root is set, so a rerun doesn't ask again.
5. Adds a cron line that runs the collector every 15 minutes, and runs it once right away so your cards are there when you open the dashboard. The collector's last run is logged to `SparkRoot/collector.log`.

Options:

- **`--root DIR`:** another SparkRoot.
- **`--port PORT`:** another port.
- **`--image IMAGE`:** another image, such as a pinned version (`nineonefour/spark:0.1.0`) or one you built from the repo (`docker build -t spark .`, then `--image spark`). A name without a `/` is never pulled.
- **Login:** set `SPARK_USERNAME` and `SPARK_PASSWORD` (at least 15 characters) when you run it: `SPARK_USERNAME=me SPARK_PASSWORD=... ./setup.sh`.

The dashboard is at http://localhost:9140.

**By hand instead.** Other settings, such as `SPARK_ALLOW_NETWORK`, need the container started yourself. Then run `setup.sh`: it keeps them, and does the links, scan root and cron:

```sh
mkdir -p ~/Documents/Spark
docker run -d --name spark \
  -p 127.0.0.1:9140:9140 \
  --user "$(id -u):$(id -g)" \
  -v ~/Documents/Spark:/spark \
  --restart unless-stopped \
  nineonefour/spark
```

- **`-p`:** keep `127.0.0.1:` unless login is on. Without login, anyone who can reach the page can change the settings.
- **Other machines:** a local answers only at `localhost` (or `127.0.0.1`). To open it by another name or address, set `-e SPARK_ALLOW_NETWORK=true`, and turn login on.
- **Compose:** copy [`docker-compose.example.yml`](docker-compose.example.yml) to `docker-compose.yml`, set the path and `user:`, and run `docker compose up -d`. Then run `~/Documents/Spark/setup.sh --host-only` for the links, scan root and cron. Without `--host-only` it would start a second container, since Compose names the container differently.

## 2. Add scan roots

`setup.sh` adds the first one. To add more, or if you skipped it, open **Settings** on the dashboard and add the folders to scan, such as `~/Projects`. Each path must start with `/` or `~/`, and `~` means your home folder on the host. The collector picks them up on its next run. To run it right away:

```sh
python3 ~/Documents/Spark/collector.py
```

## 3. Use it

In any project, tell your coding agent `Spark, go`. The skill writes `spark.md`, the collector copies it into `Projects/`, and the card appears.

A new card starts at priority 3 (When I can). Change its priority, or archive the project, on its page. Archived projects are hidden; unarchive them under **Settings → Archived**.

To pass a project to someone else, tell your coding agent `Spark, handoff`. It writes `handoff.md` next to `spark.md`: everything a new owner needs to take the project over. It asks first whether the previous owner is there to answer questions; if not, it drafts from the repo and marks what it could not confirm. The file stays in the repo; the dashboard doesn't show it.

## Sharing with a team

A team runs one Spark in **remote** mode on a server, and each person's own Spark (**local** mode, the default) pushes to it. Each push is tagged with the person's username, so a shared project shows one card with a tab per person.

On the server:

```sh
docker run -d --name spark-remote \
  -p 127.0.0.1:9140:9140 \
  --user "$(id -u):$(id -g)" \
  -v /srv/spark:/spark \
  -e SPARK_MODE=remote -e SPARK_USERNAME=admin -e SPARK_PASSWORD=... \
  -e SPARK_URL=https://spark.example.com \
  -e SPARK_TRUSTED_PROXIES=172.17.0.1 \
  --restart unless-stopped \
  nineonefour/spark
```

Put it behind HTTPS (for example Caddy). `SPARK_URL` is the address people open; Spark then refuses any other host name and builds invite links from it. `SPARK_TRUSTED_PROXIES` is your proxy's address as Spark sees it (`172.17.0.1` for a proxy on the Docker host); if it's wrong, the log names the address it saw (`security: ignoring proxy headers from ...`). Then:

1. **Admin:** under **Settings**, pick the project types the remote accepts (or accept all), and under **People** create an invite link for each person. Send the link yourself; it works once, for 7 days. **Remove** next to a person ends their login, sessions and API keys. The projects they pushed stay, renamed to `deleted-<name>` (so the username can be invited again and start clean), and are archived 30 days later. Usernames starting with `deleted-` are kept for this.
2. **Each person:** open the link, set a password, then under **Account** create an API key for each machine. A key is shown once. **Account** also changes your password (which logs out your other sessions) and logs you out everywhere.
3. **On each machine:** in the local Spark's **Settings → Remotes**, add the remote's URL and the key, then tick the project types to send there.

From then on, local pushes a project within a minute of its content, priority or archive changing, and fetches priorities set on the remote every 15 minutes (the last change to reach the remote wins). Archiving locally archives on the remote too; archiving on the remote only hides it there. Only the file's owner or the admin can change its priority or archive it on the remote, and only the admin changes types and colors.

If a remote loses its files, **Push everything again** next to it mirrors this machine to it: every project it has ever sent there, plus any new ones, goes again with its content, priority and archive flag, replacing the remote's. A project archived before it was ever pushed stays local.

### On the Internet

A remote that anyone can reach needs a few things Spark can't do for you. Its defaults are already strict (password length, login and API-key penalties, rate and size limits); this list is the rest.

1. **HTTPS in front.** Run a proxy such as Caddy on the server and keep Spark's port on `127.0.0.1` (`-p 127.0.0.1:9140:9140`), so the proxy is the only way in.
2. **`SPARK_URL`** set to the address people open. Spark then refuses other host names, builds invite links from it, marks cookies Secure and sends HSTS.
3. **`SPARK_TRUSTED_PROXIES`** set to the proxy's address as Spark sees it, so penalties count the real client's address, not the proxy's. If unsure, start without it and look for `security: ignoring proxy headers from ...` in the log.
4. **A long admin password** (at least 15 characters; Spark won't start with less).
5. **Read the security log now and then:** `docker logs spark-remote 2>&1 | grep security:`. Unlock people with `web unlock` (see [Locked out](#locked-out)).
6. **Keep the image current:** `docker pull nineonefour/spark`, then remove and rerun the container with the same flags. `setup.sh` sets up a local Spark only, so it doesn't do this for a remote.

Every limit can be loosened with the settings below; Spark logs a warning at start for each one that is.

## Settings

All settings are files in `SparkRoot/Config/`, edited on the settings page or by hand:

| File | Holds |
|---|---|
| `scan_roots.json` | Folders the collector scans, for example `["~/Projects"]` |
| `project_types.json` | Allowed project types, each with a card color, for example `[{"name": "side-project", "color": "#64748b"}]` |
| `priority_colors.json` | Card color for each priority 1–5, for example `{"1": "#dc2626", ...}` |
| `state.json` | Each project's priority and archive flag, set on the project page. A new project starts at priority 3 |
| `remotes.json` | Local only: remotes to push to, with their API keys |
| `accounts.json` | Login accounts (bcrypt passwords, hashed API keys), and on remote, pending invites |
| `remote.json` | Remote only: `{"accept_all_types": true}` to accept every project type; `max_projects_per_account` (default 50, `0` for no limit, also on the Settings page) caps each person's projects, archived ones included |
| `session_key.json` | Derives the form tokens that stop cross-site posts |
| `sessions.json` | Open login sessions (only hashes of their ids). To log everyone out, stop Spark, delete it, and start Spark |
| `lockouts.json` | Failed logins per account, and wrong API keys per address. Cleared by a correct password or key, or by `web unlock` (see [Locked out](#locked-out)) |

The web app creates any missing file with its defaults on start. A snapshot whose `project_type` isn't listed is hidden.

| Container setting | Default | |
|---|---|---|
| `SPARK_MODE` | `local` | `remote` for a team server (see [Sharing with a team](#sharing-with-a-team)) |
| `SPARK_USERNAME`, `SPARK_PASSWORD` | unset | Set both (`-e SPARK_USERNAME=me -e SPARK_PASSWORD=...`) to require login. Leave both unset for no login. Required on remote, where this is the admin; the username uses lowercase letters, digits and hyphens. Spark won't start if the password is shorter than `SPARK_MIN_PASSWORD_LENGTH` |
| `SPARK_MIN_PASSWORD_LENGTH` | `15` | Shortest password allowed, in characters, for `SPARK_PASSWORD` and new passwords. At most 72 bytes either way. Existing shorter passwords keep working until changed |
| `SPARK_PENALTY_START` | `4` | Failed logins per account (and wrong API keys per address) that wait 2 seconds each. The next waits 30 seconds, then 1, 2, 4, 8, 16 and 32 minutes. `0` means 2 seconds every time. An attempt during a wait isn't checked or counted |
| `SPARK_LOCKOUT_AFTER` | start + 7 (`11`) | The failed login that locks the account until `web unlock`. No lock by default when the start is `0`. Example: five tries 2 seconds apart, then locked: `SPARK_PENALTY_START=0`, `SPARK_LOCKOUT_AFTER=5` |
| `SPARK_LOCKOUT` | `on` | `off`: failed logins only wait, never lock |
| `SPARK_API_RATE` | `120` | Remote: valid API calls per person per minute; over it, "too many requests". `0` means no limit. Locals pace themselves to the remote's limit |
| `SPARK_MAX_FILE_KB` | `128` | Largest snapshot, in KB: bigger files aren't shown, pushed, or accepted by a remote |
| `SPARK_ALLOW_HTTP_REMOTES` | `false` | Local: `http://` remotes work only on private addresses (localhost, `10.x`, `172.16–31.x`, `192.168.x`, Tailscale `100.64–127.x`). `true` allows any |
| `SPARK_SESSION_IDLE` | `24h` | A login session ends after this long without use (a Go duration: `30m`, `8h`). Sessions also end 30 days after login |
| `SPARK_ADDR` | `:9140` | Listen address inside the container |
| `SPARK_URL` | unset | The address people open, like `https://spark.example.com`. Strongly recommended on a remote that faces the Internet. When set, Spark answers only to that host name, and invite links, the cross-site check, the Secure cookie flag and HSTS (for `https`) come from it |
| `SPARK_ALLOW_NETWORK` | `false` | Local only. `true` lets a local answer to any host name, not just `localhost`, `127.0.0.1` and `[::1]`. Turn login on as well |
| `SPARK_TRUSTED_PROXIES` | unset | Addresses or ranges of your HTTPS proxy, separated by commas, like `172.17.0.1` or `172.16.0.0/12`. Spark believes `X-Forwarded-For` and `X-Forwarded-Proto` only from these |
| `SPARK_ROOT` | `/spark` | SparkRoot inside the container |

Spark logs a warning at start for each setting looser than its default, and runs anyway. Security events (logins, lockouts, password changes, keys, invites, removed accounts, wrong API keys, API throttling and blocked addresses) go to the normal log as lines starting `security:`, with the account and the client's address.

### Locked out

Too many failed logins lock an account. Clear it with:

```sh
docker exec spark web unlock <username>
```

Without Docker, run `./web/web unlock <username>` with the same settings as the server. A correct password also clears the count once any wait is over.

Wrong API keys count per address the same way. A blocked address gets "this address is blocked"; clear it with `web unlock --ip <address>`.

## Updating

Rerun `setup.sh`. It pulls the latest image:

```sh
git pull
./setup.sh
```

It recreates the container only when the image is newer or an option changed, and takes the folder, port, image and login from the old container, so a plain rerun keeps them. A container on a local build (`--image spark`) stays on it and isn't pulled; pass `--image nineonefour/spark` to move back. Options you added to a hand-made container with `docker run` are kept if they are `-e` settings; anything else (extra mounts, networks) is not. Recreating loses nothing: all of Spark's data is in SparkRoot.

On every start the container overwrites `collector.py`, `setup.sh`, `INSTALL.md`, `Skill/` and `HandoffSkill/` with the image's copies, so don't edit those; customize through `Config/` instead. `Config/` and `Projects/` are never overwritten.

## Removing a project

Nothing is deleted automatically. Delete its file from `SparkRoot/Projects/` by hand. Renaming a project or changing its type leaves the old card until you delete the old file.

Two projects with the same name and type would share a filename: the collector copies the first, skips the rest, and logs a warning in `collector.log`.

## Without Docker

Build the web app (Go 1.27 or newer) and point it at SparkRoot:

```sh
(cd web && go build -o web .)
SPARK_ROOT=~/Documents/Spark ./web/web    # http://127.0.0.1:9140
```

Then copy `collector/collector.py`, `setup.sh`, `skill/` (as `Skill/`) and `handoff-skill/` (as `HandoffSkill/`) into SparkRoot yourself, and run `SparkRoot/setup.sh --host-only` for the links, scan root and cron. Without `SPARK_ROOT`, the web app uses the folder its binary sits in. `web/web.env.example` lists the settings; pass the file with `-config`.
