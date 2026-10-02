# Phase 5 Plan: Hardening

Status: designed 2026-10-01 (decisions below); steps 1–2 built 2026-10-02.
Decisions are also in `.mex/context/decisions.md`, under "Phase 5".

## What phase 5 is

Raise Spark's security so that a remote (or a local) exposed to the Internet withstands the
OWASP Top 10:2025 and the OWASP API Security Top 10.

**Out of scope:** TLS certificates and hosting. Spark runs behind whatever proxy the operator
sets up; its job ends at accepting a host name (`SPARK_URL`).

**Rule for every control:** safe, best-practice defaults, and every limit can be loosened by the
person running Spark (an env setting, or the Settings page). Spark logs a warning at startup when
a default is loosened and keeps running. It is not the security police.

## Audit findings (2026-10-01)

High:
1. Go 1.23 is out of support: `govulncheck` reports 32 reachable standard library vulnerabilities
   (`net/http`, `html/template`, `net/url`, `crypto/tls`, ...) plus `golang.org/x/net`. `alpine:3.20`
   is out of support too.
2. Unlimited pushes: any API key can push any number of 1 MB files, filling the disk, and every
   page reparses them all.
3. One global login throttle: failed logins queue behind one 1 s mutex, so one attacker blocks
   every login; no per-account limit.
4. No security logging: logins, key changes, invites and removals leave no trace.

Medium:

5. Sessions can't be revoked (logout only deletes the cookie; 30 days); invited users can't
   change their password.
6. The env admin password has no minimum length.
7. Invite links, the Origin check and the Secure cookie flag trust the request's `Host` and
   `X-Forwarded-Proto`. Also DNS rebinding: a website can reach a login-off local through the
   visitor's browser.
8. A local reachable without login lets anyone add a remote and receive the snapshots.

Low:

9. No form token on the login form (login CSRF).
10. CSP lacks `base-uri` and `form-action`; no HSTS.
11. Fonts load from Google; base images unpinned.
12. API keys never expire and show no last use; local accepts `http://` remotes on any address.

Already sound: goldmark escaping + bluemonday + CSP, pushed-id checks (no path traversal),
form tokens, bcrypt, hashed keys and invites, server timeouts, per-user API scoping.

## Locked decisions

1. **Fix all 12 findings**, plus DNS rebinding (7). CI stays a later phase.
2. **Login limits count per account; no IP addresses.** Failures follow the penalty schedule (5).
   A refused attempt (during a wait or a lock) is not checked and not counted. A correct password
   resets the count.
3. **Unlocking:** `docker exec spark web unlock <username>` (and `web unlock --ip <address>` for the
   API). Without Docker, `./web/web unlock ...`. Lockout state lives in `Config/lockouts.json`, so
   the command, a separate process, can clear it while the server runs.
4. **The API counts by IP.** `SPARK_TRUSTED_PROXIES` lists the proxy's addresses or ranges
   (`127.0.0.1`, `172.17.0.1`, `172.16.0.0/12`). Spark believes `X-Forwarded-For` only from those;
   otherwise it uses the connection's address. Proxy headers from an untrusted address are
   logged with that address, so the operator can copy it into the setting.
5. **Penalty schedule**, the same for login (per account) and wrong API keys (per IP):

   | Setting | Default | Meaning |
   |---|---|---|
   | `SPARK_PENALTY_START` | 4 | Failures before it wait 2 s each; this one waits 30 s; the next 6 wait 1, 2, 4, 8, 16, 32 min. 0 = 2 s forever |
   | `SPARK_LOCKOUT_AFTER` | penalty start + 7 (11); none when the start is 0 | The wrong attempt that locks, until `web unlock` |
   | `SPARK_LOCKOUT` | on | `off` = waits only, never a lock |

   Example: five 2-second attempts, then locked = `SPARK_PENALTY_START=0`, `SPARK_LOCKOUT_AFTER=5`.
6. **Valid API calls: 120 per account per minute** (`SPARK_API_RATE`). Over it, "too many
   requests". The remote states its limits in `GET /api/types`; local paces itself under them
   (a 300-file resync takes about 2½ minutes) and never sends a file it knows is too large.
7. **Storage:** 128 KB per file (`SPARK_MAX_FILE_KB`), on both ends. Projects per account: one
   number for everyone, on the remote's Settings page (`Config/remote.json`), default 50. Archived
   files count; retired (`deleted-…`) files don't. Over the cap a new project is refused with a
   clear message; updates to existing files still work.
8. **Security events go to the normal log**, prefixed `security:`: login success and failure,
   lockout, unlock, key created and revoked, invite created, revoked and used, account removed,
   password changed, API throttled, API IP blocked. Each line names the account and the IP.
9. **Server-side sessions** in `Config/sessions.json` (only hashes of session ids). Logging out
   deletes the session; the account page gets "Log out everywhere"; removing an account drops its
   sessions. Sessions end after **24 hours of inactivity** (`SPARK_SESSION_IDLE`); last seen is
   written at most every few minutes.
10. **Change password** on the account page for invited users (asks for the current one; logs out
    their other sessions). The env admin's password comes only from `SPARK_PASSWORD`.
11. **Passwords: at least 15 characters** (`SPARK_MIN_PASSWORD_LENGTH`), at most 72 bytes, the env
    admin included. Spark won't start if `SPARK_PASSWORD` is shorter than the minimum. Existing
    shorter passwords keep working until changed.
12. **`SPARK_URL`, optional and strongly recommended for Internet exposure.** When set: invite
    links, the Origin check, the Secure cookie flag and HSTS come from it, and requests for any
    other host name are refused. When unset: today's behavior, and a remote logs a warning.
13. **A local accepts only `localhost` host names by default** (`localhost`, `127.0.0.1`, `[::1]`).
    `SPARK_ALLOW_NETWORK=true` allows others; login stays optional but strongly recommended, with a
    startup warning when the flag is on and login is off.
14. **API keys don't expire.** The account page shows each key's last use (written at most hourly)
    and marks keys unused for 90 days.
15. **`http://` remotes only on private addresses** (localhost, `10.x`, `172.16–31.x`, `192.168.x`,
    Tailscale `100.64–127.x`); `SPARK_ALLOW_HTTP_REMOTES=true` allows any.
16. **Fonts are bundled** (both SIL OFL); the CSP drops Google.
17. **Base images pinned by version tag**, on current Go and Alpine; `go.mod` and every dependency
    current, `govulncheck` clean.
18. **Mechanical:** a form token on the login form; CSP `base-uri 'none'` and `form-action 'self'`;
    HSTS when `SPARK_URL` is `https`.

## Steps

1. **Toolchain** (built 2026-10-02: Go 1.27.1, Alpine 3.24.2): Go, Alpine and dependencies current; `go.mod`, `Dockerfile`, the
   e2e runner and the docs' Go version; `govulncheck` clean.
2. **Settings and the edge** (built 2026-10-02, `web/edge.go`): the new env settings and startup warnings;
   `SPARK_URL`, the host check, `SPARK_ALLOW_NETWORK`; trusted proxies and the client IP; CSP,
   HSTS, bundled fonts.
3. **Login** (about half a day): the penalty schedule and `Config/lockouts.json`, the `web unlock`
   subcommand, password length rules, the login form token, `security:` log lines.
4. **Sessions** (about half a day): `Config/sessions.json`, idle expiry, real logout, "Log out
   everywhere", change password.
5. **API** (about a day): per-IP penalties, per-account rate, file size and project caps, limits
   in `/api/types`, local pacing, the `http://` rule, key last use.
6. **Tests and docs** (about half a day): unit tests for the schedule, IP parsing and sessions;
   e2e scenarios for lockout, unlock, rate and caps; `INSTALL.md` settings table and an
   Internet-exposure section; MEX scaffold.

## Later

- CI: `gofmt`, `go vet`, `go test` and `govulncheck` on every push and weekly.
