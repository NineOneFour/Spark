# Installing Spark

Spark has three pieces:

- **The skill** writes `spark.md` in a project when you say `Spark, go`.
- **The collector** finds `spark.md` files in your source folders and copies them to one place, as `<machine-id>__<folder>.md`.
- **The web app** shows what the collector copied.

Pick one of three ways to run the collector and web app:

| Route | Use it when | Transport |
|---|---|---|
| [Localhost](#localhost) | Everything is on one machine | Collector copies into a local folder |
| [Docker](#docker) | You want it in a container | Same, inside the container |
| [Central server](#central-server) | Projects live on several machines | Collectors upload to a Samba share |

The examples below are starting points. Adjust paths, users, and addresses to fit your setup.

## Install the skill

Needed for every route.

```sh
git clone git@github.com:NineOneFour/Spark.git
ln -s "$PWD/Spark/skill" ~/.claude/skills/spark
```

## Build

Needed for localhost and the central server. Docker builds for you.

Requires Go 1.23 or newer.

```sh
cd Spark
(cd collector && go build -o collector .)
(cd web && go build -o web .)
```

Both are static binaries with no runtime dependencies. To build for another machine, set `GOOS` and `GOARCH`. For example, use `GOOS=linux GOARCH=arm64` for a Raspberry Pi.

## Configuration

Both binaries read settings from environment variables. They can also read an env file (`-config path`), and environment variables win over the file. Commented examples: [`collector/collector.env.example`](collector/collector.env.example) and [`web/web.env.example`](web/web.env.example).

| Collector | Default | |
|---|---|---|
| `SCAN_ROOT` | required | Folders to scan, comma-separated |
| `MACHINE_ID` | `local` | Prefix on every file. Must be unique per machine and must not contain `__` |
| `TARGET_DIR` | `projects/` next to the binary | Local folder to copy into |
| `SMB_HOST`, `SMB_SHARE`, `SMB_USER`, `SMB_PASSWORD` | unset | Upload to a Samba share instead. Setting `SMB_HOST` switches to this |

| Web app | Default | |
|---|---|---|
| `SPARK_DATA_DIR` | `projects/` next to the binary | Folder to read |
| `SPARK_ADDR` | `127.0.0.1:8080` | Listen address |
| `SPARK_USERNAME`, `SPARK_PASSWORD` | unset | Set both to require login. Leave both unset for no login |
| `SPARK_MERGE` | unset | Folder names to merge across machines, comma-separated. The newest snapshot wins |

**Collector deletions:** the collector removes files with its own `MACHINE_ID` prefix that it did not find on this run, so deleted projects disappear. If a run finds nothing at all, it deletes nothing, since that usually means a wrong `SCAN_ROOT`.

**Login:** it is off unless you set a username and password. Turn it on for anything reachable from the Internet.

## Localhost

The collector and web app share a `projects/` folder next to the binaries, so no configuration is needed beyond where your projects live.

```sh
mkdir -p ~/spark
cp collector/collector web/web ~/spark/

SCAN_ROOT=~/code,~/work ~/spark/collector   # copies into ~/spark/projects
~/spark/web                                 # http://localhost:8080
```

To run the collector every 15 minutes, install the systemd user timer. Your settings go in `~/.config/spark/collector.env`.

```sh
install -Dm755 ~/spark/collector ~/.local/bin/spark-collector
install -Dm644 collector/systemd/spark-collector.{service,timer} -t ~/.config/systemd/user/
mkdir -p ~/.config/spark
printf 'SCAN_ROOT=~/code\nTARGET_DIR=~/spark/projects\n' > ~/.config/spark/collector.env
systemctl --user daemon-reload
systemctl --user enable --now spark-collector.timer
```

A user service for the web app works the same way. Or just run it when you want it.

## Docker

One container runs the web app, plus the collector every 15 minutes.

- **Sources:** mount each folder you want scanned under `/sources`, read-only.
- **Data:** mount a host folder at `/projects` to hold the snapshots.

```sh
mkdir -p ~/spark-data
docker build -t spark .
docker run -d --name spark \
  -p 127.0.0.1:8080:8080 \
  --user "$(id -u):$(id -g)" \
  -v ~/spark-data:/projects \
  -v ~/code:/sources/code:ro \
  -v ~/work:/sources/work:ro \
  -e SPARK_USERNAME=me -e SPARK_PASSWORD=change-me \
  --restart unless-stopped \
  spark
```

- **`--user`:** keeps the snapshot files owned by you instead of root. Create the data folder first; if Docker creates it, it's owned by root and the collector can't write to it.
- **`COLLECT_INTERVAL`:** sets the seconds between collector runs (default `900`). Set it to `0` to turn the collector off, for example when snapshots arrive another way.
- **`-p`:** drop the `127.0.0.1:` to reach it from other machines.

For Compose, copy [`docker-compose.example.yml`](docker-compose.example.yml) to `docker-compose.yml`, edit the paths and login, and run `docker compose up -d`.

## Central server

Use this when projects live on several machines. Each machine runs a collector that uploads over SMB to a small Linux server. The web app on that server reads the files, and Caddy puts it on the Internet.

```text
laptop ──┐
desktop ─┼── SMB (LAN only) ──► /srv/spark/projects ──► web app ──► Caddy ──► you
work ────┘
```

**Keep the paths separate.** The Samba account can write only to the data folder. The web app runs as its own account with read-only access. That way, a compromised web app can't change snapshots.

### 1. Accounts and folder

On Debian or Ubuntu, for example:

```sh
useradd --system --no-create-home --shell /usr/sbin/nologin spark        # Samba writer
useradd --system --no-create-home --shell /usr/sbin/nologin spark-web    # web app
mkdir -p /srv/spark/projects
chown spark:spark /srv/spark/projects
chmod 755 /srv/spark/projects     # spark-web can read, not write
smbpasswd -a spark
```

### 2. Samba

A share limited to your LAN. For example, in `/etc/samba/smb.conf`:

```ini
[global]
   hosts allow = 192.168.1. 127.
   hosts deny = ALL
   server min protocol = SMB2

[projects]
   path = /srv/spark/projects
   valid users = spark
   read only = no
   browseable = no
   create mask = 0644
```

Also block port 445 from anywhere outside the LAN in the host firewall. Samba's own settings are not enough on their own.

### 3. Web app

Install the binary and config:

```sh
install -Dm755 web/web /usr/local/bin/spark-web
install -Dm640 -g spark-web web/web.env.example /etc/spark/web.env   # then edit it
```

In `/etc/spark/web.env`, set at least:

```sh
SPARK_DATA_DIR=/srv/spark/projects
SPARK_ADDR=127.0.0.1:8080          # or a LAN address if Caddy runs on another box
SPARK_USERNAME=me
SPARK_PASSWORD=a-long-password
```

Then add a systemd service, for example `/etc/systemd/system/spark-web.service`:

```ini
[Unit]
Description=Spark web app
After=network.target

[Service]
User=spark-web
ExecStart=/usr/local/bin/spark-web -config /etc/spark/web.env
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadOnlyPaths=/srv/spark/projects

[Install]
WantedBy=multi-user.target
```

Start it:

```sh
systemctl daemon-reload
systemctl enable --now spark-web
```

### 4. Caddy

Point Caddy at the web app. Caddy handles HTTPS and sets `X-Forwarded-Proto`, which Spark uses to mark its login cookie secure.

```caddyfile
spark.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

If Caddy runs on a different machine, point `reverse_proxy` at the Spark server's LAN address. Then firewall port 8080 so only Caddy can reach it.

### 5. Collectors

On each machine with projects, build the collector and set it up with the systemd user timer as in [Localhost](#localhost). Use SMB settings in `~/.config/spark/collector.env`:

```sh
MACHINE_ID=work-laptop
SCAN_ROOT=~/Projects
SMB_HOST=spark.lan
SMB_SHARE=projects
SMB_USER=spark
SMB_PASSWORD=the-smbpasswd-password
```

Run it once by hand to check:

```sh
~/.local/bin/spark-collector
journalctl --user -u spark-collector   # later runs
```
