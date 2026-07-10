# Public Deployment with HTTPS

This guide walks through deploying the ccrouter standalone server to a public-facing host (e.g. Alibaba Cloud Simple Application Server in Korea / Hong Kong / Singapore) with built-in HTTPS, so Claude Code on a laptop can point `ANTHROPIC_BASE_URL` at an HTTPS endpoint you control — no separate reverse proxy required.

## When to use this

- You want to use Claude Code from a laptop in one region against providers reachable from another region (e.g. mainland-China providers from outside China).
- You want a fixed HTTPS endpoint for `ANTHROPIC_BASE_URL` instead of a localhost URL.
- You don't want to run nginx/Caddy/Apache as a separate TLS terminator.

If you only need ccrouter locally (laptop-only), skip this guide — `ccrouter code` already does the right thing over loopback.

> **Fast path:** `sudo ccrouter setup server` is a guided installer that walks through everything below — bind address, TLS mode (Let's Encrypt or manual cert), system vs user level, and provider API keys (each validated with a 1-token test request) — then writes the config, the systemd unit, and starts the service. Real keys land in `~/.cc-modelrouter/shell_env.sh` (mode 0600) and `/etc/cc-modelrouter/service.env` (root:ccrouter, mode 0640); `config.json` stores only `${...}` placeholders. This guide is the manual equivalent; use it when you need full control or are debugging what the installer produced.

## How HTTPS mode works

ccrouter accepts HTTPS directly on the configured `--port`. Two mutually-exclusive certificate sources:

| Mode | Flag | When to use |
|---|---|---|
| Manual cert files | `--tls-cert` + `--tls-key` | You already have PEM cert files (from `certbot`, your own CA, or a cloud cert manager). |
| Let's Encrypt autocert | `--tls-domain` | You have a domain name pointing at the server and want certs issued + renewed automatically. |

`--tls-redirect` (forced on with `--tls-domain`) opens `:80` and 301-redirects HTTP→HTTPS. In autocert mode it also serves the ACME http-01 challenge.

Auto-restart is unaffected: `ccrouter` re-execs itself with the same `os.Args` on idle, so the TLS flags survive untouched.

## Prerequisites

1. **A public host** — e.g. Alibaba Cloud SAS with a public IP.
2. **Two firewalls open for inbound TCP**:
   - The cloud security group (Alibaba: its own panel, separate from the OS).
   - The in-VM firewall (`ufw` / `firewalmd` / iptables).
   - Open `:443` (HTTPS) always, and `:80` (HTTP) if you use `--tls-redirect` or `--tls-domain`.
3. **A domain name** (only for autocert mode) with an A record pointing at the host's public IP.
4. **ccrouter installed** — see `scripts/install.sh` (works on the Alibaba Cloud network with `GITHUB_MIRROR`).

## Recipe A — Let's Encrypt autocert (zero manual cert handling)

```bash
# 1. On the server: bind 0.0.0.0:443, autocert for your domain.
#    (:80 is opened automatically to serve the ACME challenge + redirect.)
sudo setcap CAP_NET_BIND_SERVICE=+eip $(which ccrouter)

ccrouter start -H 0.0.0.0 --port 443 --tls-domain=api.example.com
```

On first boot ccrouter talks to Let's Encrypt, issues a cert for `api.example.com`, caches it at `~/.cc-modelrouter/letsencrypt/`, and starts serving HTTPS. Renewal is automatic.

The startup banner prints the URL clients should use:
```
export ANTHROPIC_BASE_URL=https://api.example.com:443
```

**From your laptop** (point Claude Code at the public endpoint):
```bash
export ANTHROPIC_BASE_URL=https://api.example.com
export ANTHROPIC_API_KEY=<your-ccrouter-api-key>
claude
```

## Recipe B — Manual cert files (e.g. certbot)

Provision a cert with your preferred tool, e.g.:
```bash
sudo certbot certonly --standalone -d api.example.com
# Files land in /etc/letsencrypt/live/api.example.com/{fullchain.pem,privkey.pem}
```

Then start ccrouter pointing at those files, with `--tls-redirect` so HTTP clients get sent to HTTPS:
```bash
sudo setcap CAP_NET_BIND_SERVICE=+eip $(which ccrouter)

ccrouter start -H 0.0.0.0 --port 443 \
  --tls-cert=/etc/letsencrypt/live/api.example.com/fullchain.pem \
  --tls-key=/etc/letsencrypt/live/api.example.com/privkey.pem \
  --tls-redirect
```

You handle renewal yourself — set up a cron / systemd timer to run `certbot renew` and either restart ccrouter or use `--tls-cert`/`--tls-key` paths that point at live symlinks (`certbot renew` updates the file in place; ccrouter reads the cert on each connection, so no restart needed for cert rotation in manual mode).

## Recipe C — Config file instead of CLI flags

If you'd rather bake the HTTPS config into `~/.cc-modelrouter/config.json`:

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 443,
    "tls": {
      "domain": "api.example.com"
    }
  },
  "providers": { },
  "router": { }
}
```

Then just:
```bash
sudo setcap CAP_NET_BIND_SERVICE=+eip $(which ccrouter)
ccrouter start
```

CLI TLS flags override these fields when both are set.

## Running as a systemd service

For production, run ccrouter under systemd so it starts on boot and restarts on crash.

> **Shortcut:** `ccrouter setup server` (see [cli-reference.md](cli-reference.md#ccrouter-setup-server)) automates this entire flow — TLS mode, service level, API-key validation, unit-file generation, and `systemctl enable --now`. The hand-written unit below is the same shape the installer produces; use it when you need full manual control.

```bash
sudo tee /etc/systemd/system/ccrouter.service > /dev/null <<'UNIT'
[Unit]
Description=cc-modelrouter standalone server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ccrouter
Group=ccrouter
# Allow binding ports 80 and 443 without root
AmbientCapabilities=CAP_NET_BIND_SERVICE
# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/home/ccrouter/.cc-modelrouter
# Pin HOME and CCROUTER_DATA_DIR explicitly — ProtectHome=true repoints
# $HOME under the service user, and without CCROUTER_DATA_DIR the start
# command would derive it from $HOME and risk a doubled path
# (/home/ccrouter/.cc-modelrouter/.cc-modelrouter/...). Setting both
# keeps all runtime files (usage.db, logs/, instances/, master.key,
# autocert cache) inside ReadWritePaths.
Environment=HOME=/home/ccrouter/.cc-modelrouter
Environment=CCROUTER_DATA_DIR=/home/ccrouter/.cc-modelrouter
# Where ccrouter lives (adjust if installed elsewhere)
ExecStart=/usr/local/bin/ccrouter start -H 0.0.0.0 --port 443 --tls-domain=api.example.com
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

sudo useradd --system --create-home --home-dir /home/ccrouter --shell /usr/sbin/nologin ccrouter
sudo mkdir -p /home/ccrouter/.cc-modelrouter
sudo chown -R ccrouter:ccrouter /home/ccrouter
# Data dir must be group-writable (0770) so the service user can write
# usage.db, logs/, instances/, master.key, and the autocert cache.
sudo chmod 0770 /home/ccrouter/.cc-modelrouter
sudo find /home/ccrouter/.cc-modelrouter -mindepth 1 -type d -exec chmod 0770 {} +
sudo find /home/ccrouter/.cc-modelrouter -mindepth 1 -type f -exec chmod 0660 {} +
sudo systemctl daemon-reload
sudo systemctl enable --now ccrouter
sudo journalctl -u ccrouter -f   # tail logs
```

**Notes on the unit file:**

- `User=ccrouter` runs as an unprivileged user; `AmbientCapabilities=CAP_NET_BIND_SERVICE` lets it bind `:80`/`:443` without root. (Equivalent to the `setcap` incantation used in foreground mode, but applied by the supervisor.)
- `ReadWritePaths` is limited to `~/.cc-modelrouter` (config, instance metadata, logs, Let's Encrypt cache). `ProtectSystem=strict` makes the rest of the filesystem read-only.
- Use `--tls-cert`/`--tls-key` instead of `--tls-domain` if you manage certs yourself (e.g. via a separate `certbot renew` systemd timer).
- To keep TLS settings in a config file instead of CLI flags, write them to `~/.cc-modelrouter/config.json` (`server.tls.{domain, certFile, keyFile, redirect}`) and change `ExecStart` to plain `ccrouter start`.
- `ccrouter start` has its own auto-restart-on-idle feature (`--auto-restart-idle`) — systemd's `Restart=always` covers crash-recovery AND clean-exit relaunch. The two are independent and complementary.

### Alibaba Cloud SAS checklist

1. Buy a SAS instance in the target region (Korea / Hong Kong / Singapore for GFW-friendly latency to mainland providers, or vice-versa).
2. In the SAS **security-group panel**, open **inbound TCP 443 and 80**. (The SAS panel is separate from the in-VM firewall — both must allow the ports.)
3. On the VM, open the same ports: `sudo ufw allow 80,443/tcp` (or `firewall-cmd --add-port=80/tcp --permanent && firewall-cmd --add-port=443/tcp --permanent && firewall-cmd --reload`).
4. Verify on the VM: `sudo ss -tlnp | grep -E ':(443|80)\b'` should show ccrouter bound to `0.0.0.0`.
5. Point your domain's A record at the VM's public IP. Wait for DNS to propagate, then start ccrouter with `--tls-domain`.
6. **Always enable multi-user mode + API keys** before exposing publicly (see [README - Multi-User Mode](../README.md#multi-user-mode)). Never put a no-auth ccrouter on the public internet.

## Verifying it works

**On the server:**
```bash
sudo ss -tlnp | grep -E ':(443|80)\b'   # both should show ccrouter
curl -v https://api.example.com/v1/models -H "Authorization: Bearer <key>"
```

**From a remote laptop:**
```bash
curl -v https://api.example.com/v1/models -H "Authorization: Bearer <key>"
```

**HTTP→HTTPS redirect:**
```bash
curl -vI http://api.example.com/    # should see "301 Moved Permanently" with https Location
```

## Security checklist

- Bind to `0.0.0.0` (so off-host clients can reach the server) — *required* for public deployment.
- Use HTTPS (manual certs or autocert) — without it, requests and API keys traverse the internet in plaintext.
- Require API-token authentication: set up multi-user mode (`ccrouter config` → Multi-User → API Keys) so every request must present a valid `ANTHROPIC_API_KEY`. Never expose a no-auth ccrouter on the public internet.
- Restrict the cloud security group to your IP range if you don't need global access.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `bind: permission denied` on port 443/80 | Process lacks `CAP_NET_BIND_SERVICE`. Run `sudo setcap CAP_NET_BIND_SERVICE=+eip $(which ccrouter)` or run as root. |
| Autocert never issues a cert | `:80` not reachable from the internet (ACME http-01 challenge fails). Check the cloud security group and in-VM firewall. Verify the domain's A record points at the server. |
| `tls-cert file ...: no such file` | Wrong cert path. Use absolute paths. |
| `--tls-cert and --tls-domain are mutually exclusive` | Pick one mode. You can't mix manual certs with autocert. |
| Clients see cert warnings | Self-signed cert, or autocert hasn't issued yet. Wait for the first ACME handshake or use a real domain + Let's Encrypt. |
| `Warning: TLS enabled with host "localhost"` | You set `--tls-*` but `host` is still loopback — off-host clients can't reach it. Re-bind with `-H 0.0.0.0`. |
