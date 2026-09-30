---
name: deploy-central-server
description: Deploy Spark to a dedicated Linux server (Samba share + spark-web systemd service) and point per-machine collectors at it.
triggers:
  - "deploy"
  - "central server"
  - "remote server"
  - "samba"
  - "smb"
  - "spark-web service"
edges:
  - target: context/setup.md
    condition: for env settings and local build commands
  - target: context/decisions.md
    condition: for why the writer and reader accounts are separate
  - target: patterns/debug-missing-card.md
    condition: when the server is up but a card does not appear
grounds_to: []
last_updated: 2026-09-29
---

# Deploy to a Central Server

## Context
Follows the "Central server" section of `INSTALL.md`. Two accounts on the server: `spark` (Samba writer, owns `/srv/spark/projects`) and `spark-web` (runs the web app, read-only). TLS, firewalling, and reverse proxying may be handled upstream; the web app can listen directly on `0.0.0.0:8080`.

Real env files with passwords live outside Git (for example a gitignored `secret/` folder). Never print them; pipe values straight to where they are needed.

## Steps
1. **Build locally**, not on the server (small servers lack disk for Go). Same arch: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w"` in `collector/` and `web/`.
2. **Accounts and folder:** `useradd --system` for `spark` and `spark-web`; `/srv/spark/projects` owned by `spark`, mode 755.
3. **Samba:** `apt-get install --no-install-recommends samba`; back up `smb.conf`; one `[projects]` share with `valid users = spark`, `server min protocol = SMB2`, `create mask = 0644`. Disable `nmbd`. Set the password without echoing it:
   `grep -m1 '^SMB_PASSWORD=' collector.env | cut -d= -f2- | ssh root@HOST 'IFS= read -r p; printf "%s\n%s\n" "$p" "$p" | smbpasswd -s -a spark'`
4. **Web app:** `scp` the binary to `/usr/local/bin/spark-web`; install the env file as `/etc/spark/web.env` (`root:spark-web`, 640); add the hardened unit from `INSTALL.md`. Add a drop-in `spark-web.service.d/override.conf` with `Environment=SPARK_DATA_DIR=/srv/spark/projects` and `Environment=SPARK_ADDR=0.0.0.0:8080` so the server paths win over whatever the copied env file says (env beats `-config`).
5. **Collectors (each machine):** install `~/.local/bin/spark-collector` and the systemd user units; symlink `~/.config/spark/collector.env` to the real env file; set a unique `MACHINE_ID` and `SMB_HOST`/`SMB_SHARE=projects`/`SMB_USER=spark`/`SMB_PASSWORD`. Run once by hand, then `systemctl --user enable --now spark-collector.timer`.
6. **Skill:** `ln -sfn <repo>/skill ~/.claude/skills/spark`. Without it no `spark.md` files exist and the collector uploads nothing.

## Gotchas
- `SMB_USER` must be the Samba account (`spark`), not the desktop login name. A mismatch fails with `target: response error: The attempted logon is invalid`.
- `smbpasswd -a` is interactive; use `-s` with piped input for automation.
- `found 0 spark.md file(s)` is not a server fault: the skill hasn't written a file yet, or it sits outside `SCAN_ROOT`.
- A reinstalled server keeps its IP but gets new host keys; verify the fingerprint on the console before `ssh-keygen -R`.
- Samba plus deps take roughly 180 MB; check `df -h /` on tiny containers.
- With login on, `/` returns 303 to the login page; set `X-Forwarded-Proto: https` upstream so the cookie is marked secure.

## Verify
- [ ] `systemctl is-active smbd spark-web` both `active`; `ss -tlnp` shows 445 and 8080
- [ ] `pdbedit -L` lists `spark`
- [ ] `curl -o /dev/null -w '%{http_code}' http://HOST:8080/` returns 200 (no login) or 303 (login on)
- [ ] A manual collector run logs `copied … -> <MACHINE_ID>__<folder>.md` and the file appears in `/srv/spark/projects`
- [ ] `systemctl --user list-timers spark-collector.timer` shows a next run

## Debug
Auth errors: compare `SMB_USER` with `pdbedit -L`, then reset the password with step 3. Web not starting: `journalctl -u spark-web`. Card missing after a successful upload: follow `debug-missing-card.md`.

## Update Scaffold
- [ ] Add new deploy failure causes to Gotchas above and to `.mex/context/setup.md` Common Issues
