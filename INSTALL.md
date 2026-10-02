# Installing Spark

Spark has three pieces:

- **The skill** writes `spark.md` in a project when you say `Spark, go`. A second skill, Spark Handoff, writes `handoff.md` when you say `Spark, handoff`.
- **The collector** (`collector.py`) finds `spark.md` files in your scan roots and copies them into SparkRoot.
- **The web app** shows them as cards, and has a settings page.

Everything ships in one Docker image. The container owns one host folder, **SparkRoot** (for example `~/Documents/Spark`), and fills it on start:

```text
SparkRoot/
  collector.py   the collector; run on the host by cron
  setup.sh       one-time host setup
  INSTALL.md     this file
  Skill/         the Spark skill
  HandoffSkill/  the Spark Handoff skill
  Config/        settings, plus state.json (each project's priority and archive flag)
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
- **Other machines:** a local answers only at `localhost` (or `127.0.0.1`). To open it by another name or address, set `-e SPARK_ALLOW_NETWORK=true`, and turn login on.
- **Compose:** copy [`docker-compose.example.yml`](docker-compose.example.yml) to `docker-compose.yml`, set the path and `user:`, and run `docker compose up -d`.

The dashboard is at http://localhost:8080.

## 2. Set up the host

The container never touches host folders outside SparkRoot, so two steps run on the host. Needs `python3`.

```sh
~/Documents/Spark/setup.sh
```

It links `~/.claude/skills/spark` to `SparkRoot/Skill` and `~/.claude/skills/spark-handoff` to `SparkRoot/HandoffSkill`, and adds a cron line that runs the collector every 15 minutes. The collector's last run is logged to `SparkRoot/collector.log`. Rerunning the script is safe.

## 3. Add scan roots

Open **Settings** on the dashboard and add the folders to scan, such as `~/Projects`. Each path must start with `/` or `~/`, and `~` means your home folder on the host. The collector picks them up on its next run. To run it right away:

```sh
python3 ~/Documents/Spark/collector.py
```

## 4. Use it

In any project, tell your coding agent `Spark, go`. The skill writes `spark.md`, the collector copies it into `Projects/`, and the card appears.

The priority you give the skill is only the starting value. After the card first appears, change priority, or archive the project, on its page. Archived projects are hidden; unarchive them under **Settings → Archived**.

To pass a project to someone else, tell your coding agent `Spark, handoff`. It writes `handoff.md` next to `spark.md`: everything a new owner needs to take the project over. It asks first whether the previous owner is there to answer questions; if not, it drafts from the repo and marks what it could not confirm. The file stays in the repo; the dashboard doesn't show it.

## Sharing with a team

A team runs one Spark in **remote** mode on a server, and each person's own Spark (**local** mode, the default) pushes to it. Each push is tagged with the person's username, so a shared project shows one card with a tab per person.

On the server:

```sh
docker run -d --name spark-remote \
  -p 127.0.0.1:8080:8080 \
  --user "$(id -u):$(id -g)" \
  -v /srv/spark:/spark \
  -e SPARK_MODE=remote -e SPARK_USERNAME=admin -e SPARK_PASSWORD=... \
  -e SPARK_URL=https://spark.example.com \
  -e SPARK_TRUSTED_PROXIES=172.17.0.1 \
  --restart unless-stopped \
  spark
```

Put it behind HTTPS (for example Caddy). `SPARK_URL` is the address people open; Spark then refuses any other host name and builds invite links from it. `SPARK_TRUSTED_PROXIES` is your proxy's address as Spark sees it (`172.17.0.1` for a proxy on the Docker host); if it's wrong, the log names the address it saw (`security: ignoring proxy headers from ...`). Then:

1. **Admin:** under **Settings**, pick the project types the remote accepts (or accept all), and under **People** create an invite link for each person. Send the link yourself; it works once, for 7 days. **Remove** next to a person ends their login, sessions and API keys. The projects they pushed stay, renamed to `deleted-<name>` (so the username can be invited again and start clean), and are archived 30 days later. Usernames starting with `deleted-` are kept for this.
2. **Each person:** open the link, set a password, then under **Account** create an API key for each machine. A key is shown once. **Account** also changes your password (which logs out your other sessions) and logs you out everywhere.
3. **On each machine:** in the local Spark's **Settings → Remotes**, add the remote's URL and the key, then tick the project types to send there.

From then on, local pushes a project within a minute of its content, priority or archive changing, and fetches priorities set on the remote every 15 minutes (the last change to reach the remote wins). Archiving locally archives on the remote too; archiving on the remote only hides it there. Only the file's owner or the admin can change its priority or archive it on the remote, and only the admin changes types and colors.

If a remote loses its files, **Push everything again** next to it mirrors this machine to it: every project it has ever sent there, plus any new ones, goes again with its content, priority and archive flag, replacing the remote's. A project archived before it was ever pushed stays local.

## Settings

All settings are files in `SparkRoot/Config/`, edited on the settings page or by hand:

| File | Holds |
|---|---|
| `scan_roots.json` | Folders the collector scans, for example `["~/Projects"]` |
| `project_types.json` | Allowed project types, each with a card color, for example `[{"name": "side-project", "color": "#64748b"}]` |
| `priority_colors.json` | Card color for each priority 1–5, for example `{"1": "#dc2626", ...}` |
| `state.json` | Each project's priority and archive flag, set on the project page. Seeded from the snapshot the first time it's seen |
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
| `SPARK_ADDR` | `:8080` | Listen address inside the container |
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

Pull the new image and recreate the container. On every start it overwrites `collector.py`, `setup.sh`, `INSTALL.md`, `Skill/` and `HandoffSkill/` with the image's copies, so don't edit those; customize through `Config/` instead. `Config/` and `Projects/` are never overwritten. If your install predates Spark Handoff, rerun `setup.sh` once to link it.

## Removing a project

Nothing is deleted automatically. Delete its file from `SparkRoot/Projects/` by hand. Renaming a project or changing its type leaves the old card until you delete the old file.

Two projects with the same name and type would share a filename: the collector copies the first, skips the rest, and logs a warning in `collector.log`.

## Without Docker

Build the web app (Go 1.27 or newer) and point it at SparkRoot:

```sh
(cd web && go build -o web .)
SPARK_ROOT=~/Documents/Spark ./web/web    # http://127.0.0.1:8080
```

Then copy `collector/collector.py`, `setup.sh`, `skill/` (as `Skill/`) and `handoff-skill/` (as `HandoffSkill/`) into SparkRoot yourself, and run `setup.sh`. Without `SPARK_ROOT`, the web app uses the folder its binary sits in. `web/web.env.example` lists the settings; pass the file with `-config`.

## Moving from an older Spark

Older versions allowed 8-character passwords. If `SPARK_PASSWORD` is shorter than 15, Spark won't start until you lengthen it (or lower `SPARK_MIN_PASSWORD_LENGTH`). Invited people's shorter passwords keep working.

Older versions named snapshots `<machine-id>__<folder>.md` and used a systemd timer. Delete the old data folder, disable the old timer (`systemctl --user disable --now spark-collector.timer`), and let the collector refill `Projects/` from your `spark.md` files.
