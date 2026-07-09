# cc-modelrouter

[![Go 1.24](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/iimmutable-ai/cc-modelrouter)](https://goreportcard.com/report/github.com/iimmutable-ai/cc-modelrouter)
[![CI](https://github.com/iimmutable-ai/cc-modelrouter/actions/workflows/ci.yml/badge.svg)](https://github.com/iimmutable-ai/cc-modelrouter/actions/workflows/ci.yml)

**A local API gateway for [Claude Code](https://docs.anthropic.com/en/docs/claude-code) that routes requests to any LLM provider — GLM, GPT, Claude, Gemini, Kimi, Qwen, and more.**

> Also known as: **claude-code-router** | **ccr** | **ccrouter** | **claude code api gateway** | **claude code proxy** | **claude code model router**

Code with any frontier LLM from Claude Code — no vendor lock-in, no format headaches.

```
Claude Code  -->  ccrouter (localhost)  -->  Provider API
                   (auto-routes)              (GLM, GPT, Claude, Gemini, Kimi, Qwen)
```

The vibe coding landscape moves fast. Last month's best model might not be this month's. cc-modelrouter gives you a single local proxy that auto-routes Claude Code requests to any provider, handling every API format difference transparently.

## Why cc-modelrouter?

- **The best coding model changes weekly.** Last month it was Claude, this month it might be GLM-5.1 or Kimi K2.5. Hardcoding a single provider means reconfiguring every time the landscape shifts. cc-modelrouter routes to any provider through one stable config.
- **Claude Code only speaks Anthropic API.** Every other provider speaks something different — OpenAI format, Gemini native, GLM's Anthropic-compatible dialect. cc-modelrouter handles the translation transparently.
- **Smart routing, not just proxying.** Background agents get cheaper models; deep reasoning gets the heavy hitters. Routes are detected automatically from request characteristics — think level, modality, context size.
- **Zero friction.** `ccrouter code` launches Claude Code with auto-accepted permissions. No extra flags needed.

Runs locally on localhost. API keys never leave your machine. Admin API is token-authenticated. Single Go binary, no runtime dependencies.

## How Is This Different?

| | cc-modelrouter | `ANTHROPIC_BASE_URL` | OpenRouter | LiteLLM |
|---|---|---|---|---|
| **Multi-provider** | Yes | One at a time | Yes | Yes |
| **Per-request routing** | Automatic | No | No | No |
| **Failover** | Sequential, per route | No | No | Config-based |
| **Thinking-level detection** | Built-in | No | No | No |
| **Local monitoring** | Built-in TUI | No | Dashboard only | Web UI |
| **Stack** | Single Go binary | None | SaaS | Python + Redis |

## Quick Start

### Install

**Recommended (curl):**

```bash
curl -fsSL https://raw.githubusercontent.com/iimmutable-ai/cc-modelrouter/master/scripts/install.sh | bash
```

> **Alibaba Cloud / China users:** the `curl | bash` pipe is truncated by the network path to `raw.githubusercontent.com`. Use `GOPROXY=https://goproxy.cn,direct go install ...` (below), or see [Installation Behind a GitHub Firewall](docs/troubleshooting.md#installation-fails-behind-a-github-firewall-alibaba-cloud-china-etc).

**Alternative (Go users):**

```bash
go install github.com/iimmutable-ai/cc-modelrouter/cmd/ccrouter@latest
```

**From source:**

```bash
git clone https://github.com/iimmutable-ai/cc-modelrouter
cd cc-modelrouter
make install
```

> **Note:** The first `ccrouter config` run downloads the latest provider presets from GitHub. To refresh presets later, delete `~/.cc-modelrouter/provider-presets.json` and re-run `ccrouter config`.

### Configure

The easiest way is the interactive TUI wizard:

```bash
ccrouter config
```

**Configuration Wizard:**

| | | |
|---|---|---|
| ![Main Menu](assets/config-wizard-main.png) | ![Providers](assets/config-wizard-providers.png) | ![Edit Provider](assets/config-wizard-edit-provider.png) |
| *Main menu with Providers, Routes, Proxy, Logging, and View Config options* | *Provider list with base URLs and supported models* | *Edit Provider form for name, URL, and models* |
| ![Routes](assets/config-wizard-routes.png) | ![Edit Route](assets/config-wizard-edit-route.png) | |
| *Routes view with failover chains per route* | *Add/Edit Route form for configuring provider chains* | |

Or create `~/.cc-modelrouter/config.json` manually:

```json
{
  "providers": {
    "bigmodel": {
      "apiKey": "${CCROUTER_BIGMODEL_API_KEY}",
      "baseURL": "https://open.bigmodel.cn/api/anthropic",
      "transformer": "glm_anthropic",
      "models": ["glm-5.1", "glm-4.7"]
    },
    "openrouter": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
      "baseURL": "https://openrouter.ai/api",
      "transformer": "anthropic",
      "models": ["anthropic/claude-sonnet-4.6", "openai/gpt-5.4"]
    }
  },
  "router": {
    "routes": {
      "default": "openrouter:anthropic/claude-sonnet-4.6",
      "background": "bigmodel:glm-4.7",
      "think": "openrouter:anthropic/claude-sonnet-4.6",
      "ultrathink": "bigmodel:glm-5.1"
    }
  }
}
```

### Run

```bash
ccrouter code
```

This starts the router and launches Claude Code with:
- Proxy auto-configured via `ANTHROPIC_BASE_URL`
- `--permission-mode auto` (unrestricted) applied by default — no manual approval prompts
- Pass any additional Claude Code flags after `--`:

```bash
ccrouter code -- --model claude-opus-4-6
ccrouter code --conservative    # use default permissions instead
ccrouter code -- --permission-mode default --model claude-sonnet-4-6
```

See [docs/configuration.md](docs/configuration.md) for the full configuration reference.

> Models are continuously updated — run `ccrouter config` to see the latest.

## Supported Providers & Models

| Provider | Type | Models |
|----------|------|--------|
| **Zhipu GLM** (bigmodel) | Direct | glm-4.6v, glm-4.7, glm-5-turbo, glm-5v-turbo, glm-5.1, glm-5.2 |
| **Anthropic** | Direct | claude-haiku-4.5, claude-sonnet-4.6, claude-opus-4.5, claude-opus-4.6 |
| **OpenRouter** | Aggregator | openai/gpt-5.4, openai/gpt-5.4-mini, openai/gpt-5.3-codex, google/gemini-2.5-flash, google/gemini-2.5-pro |
| **OpenRouter (Anthropic)** | Aggregator | anthropic/claude-haiku-4.5, anthropic/claude-sonnet-4.5, anthropic/claude-sonnet-4.6, anthropic/claude-opus-4.5, anthropic/claude-opus-4.6 |
| **OpenRouter (OpenAI)** | Aggregator | openai/gpt-5.4, openai/gpt-5.4-mini, openai/gpt-5.3-codex |
| **Aliyun DashScope** (alicloud) | Aggregator | MiniMax-M2.5, kimi-k2.5, qwen3-coder-plus, glm-5, glm-4.7, qwen3.7-plus, glm-5.1, glm-5.2, kimi-k2.6 |
| **Google Gemini** | Direct | gemini-2.5-flash, gemini-2.5-pro, gemini-2.0-flash |

OpenRouter provides access to Anthropic, OpenAI, and Google models through a single API key. The `openrouter-anthropic` and `openrouter-openai` presets are filtered variants for use when you want to restrict the wizard's model picker to a single vendor family.

## Smart Routing

Routes are detected automatically from request characteristics. Configure which provider:model each route uses.

| Route | Trigger | Example Use |
|-------|---------|-------------|
| `ultrathink` | `budget_tokens >= 32,000` | Deep architectural planning |
| `thinkMore` | `budget_tokens >= 10,000` | Complex refactoring |
| `think` | `budget_tokens >= 4,000` | Standard reasoning tasks |
| `background` | Background agent flag | File indexing, linting |
| `subagent` | Tool names contain "subagent" or prompt contains "subagent"/"delegate to agent" | Delegated agent tasks |
| `review` | Prompt contains "/review", "code review", or starts with "review " | Code review, PR review |
| `image` | Request contains image blocks | Screenshot analysis, UI review |
| `webSearch` | Tool names contain "web"/"search" | Research-heavy tasks |
| `longContext` | Token count > 60,000 | Large codebase analysis |
| `default` | Fallback | Everything else |

**Priority order:** Routes are checked in this order: `background` → `subagent` → `review` → `ultrathink` → `thinkMore` → `think` → `image` → `webSearch` → `longContext` → `default`.

**Thinking level cascade:** If `ultrathink` is not configured, it falls back to `thinkMore`, then `think`.

## Standalone Server Mode

Run ccrouter as a standalone server for use with any Anthropic-compatible client — not just Claude Code.

```bash
# Start on default port (8081)
ccrouter start

# Custom port and profile
ccrouter start --port 9090 --profile cost-opt

# With debug logging
ccrouter start --log-level=debug --log-destination=file
```

Point any Anthropic-compatible client at the server:

```bash
export ANTHROPIC_BASE_URL=http://localhost:8081
```

### Public Deployment (HTTPS on Alibaba Cloud SAS, etc.)

Run ccrouter as a public-facing HTTPS endpoint — for example on **Alibaba Cloud Simple Application Server** in Korea / Hong Kong / Singapore — so Claude Code on a laptop can point at an HTTPS URL you control. Built-in TLS means no separate reverse proxy.

**Quick start (Let's Encrypt autocert, one-liner):**

```bash
sudo setcap CAP_NET_BIND_SERVICE=+eip $(which ccrouter)
ccrouter start -H 0.0.0.0 --port 443 --tls-domain=api.example.com
```

`--tls-domain` issues and renews the certificate automatically via the ACME http-01 challenge (port 80 must be reachable). `--tls-redirect` is forced on. Cert cache lives at `~/.cc-modelrouter/letsencrypt/`.

**Manual cert files (if you bring your own PEM files, e.g. from certbot):**

```bash
ccrouter start -H 0.0.0.0 --port 443 \
  --tls-cert=/etc/letsencrypt/live/api.example.com/fullchain.pem \
  --tls-key=/etc/letsencrypt/live/api.example.com/privkey.pem \
  --tls-redirect
```

**Then point Claude Code (from your laptop):**

```bash
export ANTHROPIC_BASE_URL=https://api.example.com
export ANTHROPIC_API_KEY=<your-sk-ccr-key>
claude
```

**Alibaba Cloud SAS checklist:**
1. Buy a SAS instance in the target region (Korea/HK/Singapore for GFW-friendly latency to mainland providers, or vice-versa).
2. In the SAS security-group panel, open **inbound TCP 443 and 80**. (The SAS panel is separate from the in-VM firewall — both must allow the ports.)
3. On the VM, open the same ports: `sudo ufw allow 80,443/tcp` (or `firewall-cmd --add-port=80/tcp --permanent && firewall-cmd --add-port=443/tcp --permanent && firewall-cmd --reload`).
4. Verify on the VM: `sudo ss -tlnp | grep -E ':(443|80)\b'` should show ccrouter bound to `0.0.0.0`.
5. Point your domain's A record at the VM's public IP. Wait for DNS to propagate, then start ccrouter with `--tls-domain`.
6. **Always enable multi-user mode + API keys** before exposing publicly (see [Multi-User Mode](#multi-user-mode)). Never put a no-auth ccrouter on the public internet.

See [docs/deployment.md](docs/deployment.md) for the full walk-through, troubleshooting, and the config-file equivalent.

#### Guided install (`setup server`)

The fastest path from a fresh Linux VM to a running public HTTPS service is the guided installer — it walks through bind address, TLS mode, service level, and provider API keys (validating each with a 1-token test request), then writes the config, the systemd unit, and starts the service:

```bash
sudo ccrouter setup server
```

Real API keys are stored in `~/.cc-modelrouter/shell_env.sh` (mode 0600) and — for system-level installs — `/etc/cc-modelrouter/service.env` (root:ccrouter, mode 0640). `config.json` keeps only `${CCROUTER_<NAME>_API_KEY}` placeholders. See [CLI reference](docs/cli-reference.md#ccrouter-setup-server) for flags including `--dry-run`.

> **Reusing existing keys:** if `~/.cc-modelrouter/shell_env.sh` already exists (e.g. from a prior `ccrouter config` run), the installer detects it at step [4/5] and offers to reuse the keys for this install — so re-running `setup server` on a host you've already configured won't clobber your secrets. A backstop guard also refuses to overwrite a populated `shell_env.sh` with an empty provider set.

#### Running as a systemd service

For production, run ccrouter under systemd so it starts on boot and restarts on crash.

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
# Where ccrouter lives (adjust if installed elsewhere)
ExecStart=/usr/local/bin/ccrouter start -H 0.0.0.0 --port 443 --tls-domain=api.example.com
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

sudo useradd --system --create-home --home-dir /home/ccrouter --shell /usr/sbin/nologin ccrouter
sudo chown -R ccrouter:ccrouter /home/ccrouter
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

### Generating Claude Code Settings

Use `ccrouter gen settings` to generate a Claude Code `settings.local.json` that pre-configures the proxy URL and API key.

By default the command is interactive (TTY): it prompts for deployment type (Local vs Public) and, when **Public** is chosen, detects the server's public IPv4 via `api.ipify.org` (3s timeout). Detection failure is never fatal — it falls back to `localhost`. In non-interactive sessions (piped stdin) it defaults to `localhost` and makes **no** network call. Pass `--url` or `--ip` to skip the prompt entirely (scripting/offline-friendly).

```bash
# Interactive: pick Local or Public (default Local)
ccrouter gen settings --user alice

# Generate with a key directly
ccrouter gen settings --key sk-ccr-abc123

# Scripting / offline: specify the IP explicitly, no prompt
ccrouter gen settings --ip 10.0.0.5 --port 8081 --user alice

# Full URL override (highest precedence, no prompt or detection)
ccrouter gen settings --url http://myserver:8081 -o .claude/settings.local.json
```

**Flag precedence** (first match wins): `--url` → `--ip` → non-TTY localhost default → TTY interactive prompt.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | *(prompted)* | Full router URL (overrides prompt and detection) |
| `--ip` | *(prompted)* | Server IP (skips prompt and detection; offline-friendly) |
| `-p`, `--port` | `8081` | Router port |
| `--user` | | Username to look up API key from keystore |
| `--key` | | API key directly (overrides `--user`) |
| `-o`, `--output` | stdout | Output file path |

For team setups with multiple users, see [Multi-User Mode](#multi-user-mode).

## Multi-User Mode

Share a single router instance across a team. Each user authenticates with their own API key, with guaranteed capacity allocation per group and automatic provider overload detection.

### Quick Setup

Run the configuration wizard and navigate to **Multi-User** (option 6):

```bash
ccrouter config
```

1. Select **Multi-User** → enable the toggle and set Global Max Concurrency and WRED thresholds
2. Select **User Groups** → create a group (name, route profile, priority, max concurrency)
3. Select **API Keys** → create a key for each user and assign to a group
4. Choose **Save & Exit** from the main menu to persist everything

Or set up entirely from the CLI:

```bash
# Enable multi-user mode
ccrouter keys settings --enabled

# Create a group
ccrouter keys groups create --name developers --profile standard --priority 0.7 --max-concurrency 50

# Create an API key (save the key — it's shown only once)
ccrouter keys create --name alice --group developers
```

### Key Concepts

| Concept | Description |
|---------|-------------|
| **API Keys** | `sk-ccr-...` bearer tokens, set as `ANTHROPIC_API_KEY` in Claude Code |
| **User Groups** | Map keys to routing profiles with QoS settings |
| **Guaranteed Shares** | `ceil(globalMax × priorityWeight)` — guaranteed capacity per group |
| **Idle Borrowing** | Groups can borrow unused capacity from other groups |
| **WRED** | Weighted Random Early Detection drops requests when queue is full |
| **Provider AIMD** | Auto-detects 429s and adjusts concurrency limits (additive increase, multiplicative decrease) |

### CLI Commands

```bash
# Create a group
ccrouter keys groups create --name developers --profile standard --priority 0.7 --max-concurrency 50

# Create an API key (save the key — it's shown only once)
ccrouter keys create --name alice --group developers

# List keys and groups
ccrouter keys list
ccrouter keys groups list

# Revoke a key
ccrouter keys revoke <id>

# Delete a group (fails if keys reference it)
ccrouter keys groups delete <id>

# View or update multi-user settings
ccrouter keys settings
ccrouter keys settings --enabled --global-max 200 --wred-min 0.4 --wred-max 0.85

# Manage group members
ccrouter keys groups members list <group-id>
ccrouter keys groups members add <group-id> --user alice
ccrouter keys groups members remove <group-id> --user alice
```

### Admin API

All endpoints require localhost + admin token (`/_admin/`):

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/_admin/keys` | List API keys |
| `POST` | `/_admin/keys` | Create API key |
| `DELETE` | `/_admin/keys/{id}` | Revoke key |
| `GET` | `/_admin/groups` | List groups |
| `POST` | `/_admin/groups` | Create group |
| `PUT` | `/_admin/groups/{id}` | Update group |
| `DELETE` | `/_admin/groups/{id}` | Delete group |
| `GET` | `/_admin/qos` | QoS stats + provider limits |
| `POST` | `/_admin/qos/provider/{name}/reset` | Reset provider AIMD |

## QoS (Quality of Service)

When multi-user mode is enabled, ccrouter applies a QoS engine that ensures fair capacity allocation across user groups and prevents provider overload.

### How It Works

**Guaranteed Shares:** Each group gets a guaranteed minimum capacity based on its priority weight: `ceil(globalMax × priorityWeight / totalWeight)`. A group with weight 0.7 and global max 100 gets at least 70 concurrent slots.

**Idle Borrowing:** If a group isn't using its guaranteed share, other groups can borrow the unused capacity. When the owner group needs it back, borrowers are queued.

**WRED (Weighted Random Early Detection):** When a group's queue fills up, requests aren't simply dropped or accepted — WRED applies probabilistic dropping based on queue depth:
- Queue depth ≤ `wred-min` (default 50%): All requests admitted
- Queue depth ≥ `wred-max` (default 90%): All requests dropped
- Between min and max: Linear probability — drop chance increases proportionally with depth

**Provider AIMD:** Each provider has a dynamic concurrency limit that adapts to 429 (rate limit) responses:
- **Multiplicative decrease:** On sustained 429s (2+ in 60s window), halve the limit; on a single 429, reduce by 20%
- **Additive increase:** After 10 consecutive successes with no 429s, increment limit by 1
- Provider limits act as an additional cap: `effectiveCap = min(globalMax, sumOfProviderLimits)`

### Configuration

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `global-max` | int | `0` (auto → 100) | Global max concurrent requests across all groups |
| `wred-min` | float | `0.5` | Queue depth % where WRED begins dropping |
| `wred-max` | float | `0.9` | Queue depth % where WRED drops everything |
| `--priority` | float | `1.0` | Per-group priority weight (0–1) |
| `--max-concurrency` | int | `0` (unlimited) | Per-group max concurrent requests |

Configure via CLI or the configuration wizard:

```bash
# CLI
ccrouter keys settings --global-max 200 --wred-min 0.4 --wred-max 0.85
ccrouter keys groups update <id> --priority 0.8 --max-concurrency 60
```

## Configuration Wizard

The interactive TUI wizard (`ccrouter config`) provides a full-screen interface for every configuration option.

**Screens:**

| Screen | Description |
|--------|-------------|
| **Main Menu** | Central hub with navigation to all config sections |
| **Providers** | Add, edit, and delete API providers with transformer selection |
| **Routes** | Configure routing rules with profile tabs |
| **Proxy Settings** | Host, port, max retries, retry delay |
| **Logging** | Log level (debug/info/warn/error), destination (console/file), file path |
| **Multi-User** | Enable toggle, global max concurrency, WRED min/max thresholds |
| **API Keys** | List, create, and revoke API keys |
| **User Groups** | Create and manage groups with profile, priority, and concurrency limits |
| **View Config** | Read-only view of the full configuration JSON |
| **Test Connection** | Verify provider connectivity before saving |

Use arrow keys to navigate, Enter to select, Esc to go back, `d`/Del to delete items.

## Route Profiles

Switch between entire routing configurations without restarting. Perfect for toggling between "standard", "cost-optimized", or "speed-first" strategies on the fly.

**At launch:**
```bash
ccrouter code --profile cost-opt
ccrouter start --profile speed-first
```

**Hot-switch mid-session** using the `/profile` slash command in Claude Code:
```
/profile cost-opt    # switch to cheaper models
/profile standard    # switch back to default
/profile             # list all profiles
```

Profiles are defined in your config:
```json
{
  "router": {
    "profiles": {
      "standard": {
        "name": "Standard",
        "routes": {
          "default": "openrouter:anthropic/claude-sonnet-4.6",
          "ultrathink": "bigmodel:glm-5.1"
        }
      },
      "cost-opt": {
        "name": "Cost Optimized",
        "routes": {
          "default": "bigmodel:glm-4.7",
          "ultrathink": "bigmodel:glm-5.1"
        }
      }
    }
  }
}
```

## Auto-Failover

Never lose a session to a provider outage. Define failover chains per route using semicolon-separated `provider:model` pairs — ccrouter automatically tries the next provider if one fails:

```json
"default": "openrouter:anthropic/claude-sonnet-4.6;bigmodel:glm-5.1;gemini:gemini-2.5-pro"
```

If OpenRouter is down, it seamlessly falls back to GLM, then Gemini. Max attempts = 2× the number of providers in the chain.

## Live Monitor

Real-time token usage dashboard with per-route and per-model breakdowns, live log tailing, and multi-user analytics.

![Live Monitor](assets/monitor-tui.png)

### Tabs (Multi-User Mode)

When multi-user mode is enabled, three additional tabs are available:

| Tab | Key | Description |
|-----|-----|-------------|
| **ROUTES** | `o` | Token usage and request counts broken down by route |
| **USERS** | `u` | Per-user usage breakdown — requests, tokens, and active status |
| **GROUPS** | `g` | Per-group usage aggregation with member counts and capacity usage |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `q` / `Ctrl+C` | Quit |
| `c` | Toggle console log panel |
| `Space` | Pause/resume log tail |
| `↑` / `k` | Previous instance |
| `↓` / `j` | Next instance |
| `←` / `Shift+Tab` | Previous date range (TODAY → WEEK → MONTH → YTD → TTM) |
| `→` / `Tab` | Next date range |
| `r` | Force refresh |
| `1`–`7` | Toggle log level filters (VERBS, TRACE, DEBUG, INFO, WARN, ERROR, FATAL) |
| `g` | Switch to GROUPS tab (multi-user only) |
| `u` | Switch to USERS tab (multi-user only) |
| `o` | Switch to ROUTES tab (multi-user only) |

## Features

- **Config Wizard** — full-screen interactive TUI for setup (`ccrouter config`) with providers, routes, profiles, server settings, multi-user, API keys, and connectivity testing
- **Auto Permissions** — `ccrouter code` defaults to `--permission-mode auto` for zero-friction launch
- **Arg Passthrough** — pass any flags to Claude Code via `--` separator
- **Standalone Server** — run as a persistent server for any Anthropic-compatible client
- **Auto-Restart** — `ccrouter start --auto-restart-idle=30m` recycles idle daemons during a configurable time window so long-running servers release OS resources without interrupting active traffic ([docs](docs/configuration.md#auto-restart))
- **Built-in HTTPS** — `ccrouter start --tls-domain=api.example.com` (Let's Encrypt autocert) or `--tls-cert`/`--tls-key` (manual certs) serves HTTPS directly, with optional `--tls-redirect` from `:80`. For deploying to public hosts like Alibaba Cloud SAS in Korea/HK/Singapore without a reverse proxy ([docs](docs/deployment.md))
- **Guided Server Install** — `sudo ccrouter setup server` walks through bind/TLS/service level/API keys, validates each key with a live 1-token test request, then writes config + systemd unit + service.env and enables the service. Reuses existing `shell_env.sh` keys when re-run on an already-configured host ([docs](#guided-install-setup-server))
- **User-Agent Override** — the router sends the same `User-Agent` Claude Code sends to Anthropic-protocol providers by default; override via `server.userAgent` for provider-specific front-ends ([docs](docs/configuration.md#user-agent))
- **Settings Generation** — `ccrouter gen settings` generates Claude Code settings with proxy URL and API key pre-configured
- **Request Compaction** — automatic request reduction for providers with context window limits
- **Instance Isolation** — each `ccrouter code` gets its own port, PID, and log file
- **Project Config** — per-project config completely overrides global settings
- **Multi-User Support** — team sharing with API key auth, user groups mapped to routing profiles, and per-group QoS with priority queuing
- **QoS Engine** — guaranteed capacity shares with idle borrowing, WRED probabilistic dropping, and provider AIMD overload detection
- **Per-User/Per-Group Tracking** — live monitor tabs show usage breakdown by user and group
- **Usage Tracking** — SQLite-based token tracking with buffered writes

## Security

- **API keys stay local** — requests are proxied through localhost; keys never leave your machine.
- **Automatic header sanitization** — 11+ sensitive headers redacted case-insensitively (Authorization, X-Api-Key, Cookie, etc.).
- **Environment variable interpolation** — use `${VAR_NAME}` in config to keep secrets out of config files.
- **Verified by tests** — security test suite verifies secrets never appear in log output.

## CLI

| Command | Description |
|---------|-------------|
| `ccrouter code` | Start router + launch Claude Code (auto permissions) |
| `ccrouter start` | Start standalone router |
| `ccrouter stop [id]` | Stop instance (all if no ID) |
| `ccrouter restart [id]` | Restart a router instance |
| `ccrouter status` | Show running instances |
| `ccrouter clean` | Remove stale instance files |
| `ccrouter config` | Interactive config wizard (TUI) |
| `ccrouter setup server` | Guided installer for public HTTPS + systemd (Linux) |
| `ccrouter logs [id]` | Show instance logs |
| `ccrouter monitor` | Live usage monitor (TUI) |
| `ccrouter profile list` | List route profiles |
| `ccrouter profile switch <name>` | Switch profile |
| `ccrouter gen settings` | Generate Claude Code settings.json with proxy URL and API key |
| `ccrouter keys settings` | View or update multi-user settings (enabled, global-max, WRED) |
| `ccrouter keys create/list/revoke` | Manage API keys for multi-user mode |
| `ccrouter keys groups list/create/update/delete` | Manage user groups |
| `ccrouter keys groups members list/add/remove` | Manage group membership |

See [docs/cli-reference.md](docs/cli-reference.md) for the full command reference with all flags.

## Development

```bash
# Build (branch-aware: "dev" tag on dev-local, git-tag elsewhere)
make build

# Release build (stripped symbols, same branch-aware version)
make release

# Quick single-platform debug build (no Linux cross-compile)
go build -o bin/debug/ccrouter ./cmd/ccrouter

# Test
go test ./...
go test -coverprofile=coverage.out && go tool cover -html=coverage.out
go test -v ./test/security   # security tests
```

### Project Structure

```
cc-modelrouter/
├── cmd/ccrouter/              # CLI entry point
├── internal/
│   ├── auth/                  # API key management and validation (multi-user)
│   ├── cli/                   # Cobra commands
│   ├── config/                # Config loading with env var interpolation
│   ├── configwizard/          # Interactive TUI wizard (Bubble Tea)
│   ├── daemon/                # Instance management (PID files, metadata)
│   ├── interceptor/           # Request/response/streaming interceptors
│   ├── logging/               # Logging with header sanitization
│   ├── monitor/               # Live usage monitor (TUI)
│   ├── provider/              # HTTP clients for provider APIs
│   ├── proxy/                 # HTTP proxy server and request handler
│   ├── qos/                   # QoS engine with WRED and provider capacity tracking
│   ├── restartlog/            # Auto-restart outcome logging (append-only JSONL)
│   ├── router/                # Route detection and sequential failover
│   ├── setupprompt/           # Non-TUI Q&A helpers for `setup server`
│   ├── svcinstall/            # systemd unit rendering and install
│   ├── transformer/           # Format transformers (Anthropic, OpenAI, Gemini, GLM)
│   ├── usage/                 # SQLite usage tracking
│   ├── useragent/             # Claude Code SDK User-Agent propagation
│   └── version/               # Build version metadata
├── pkg/api/anthropic/         # Anthropic API type definitions
└── docs/                      # Architecture, config, transformers, troubleshooting
```

See [docs/architecture.md](docs/architecture.md) for the full architecture and [docs/transformers.md](docs/transformers.md) for transformer details. See [docs/testing.md](docs/testing.md) for test patterns and [docs/troubleshooting.md](docs/troubleshooting.md) for common issues.

PRs welcome.

## Support

If cc-modelrouter saves you from vendor lock-in, consider buying me a coffee:

| Network | Address |
|---------|---------|
| **Solana (SOL)** | `GjpzLx3aX1MvpMVprdZm2hSyzHTSFJAgjwChT1fM1uKv` |
| **Ethereum (USDC)** | `0x0402e35252476230696dc639f502C14e4c92dfD6` |

## License

[MIT](LICENSE)
