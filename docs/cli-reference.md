# CLI Reference

## Installation

**Recommended (curl):**

```bash
curl -fsSL https://raw.githubusercontent.com/iimmutable/cc-modelrouter/master/scripts/install.sh | bash
```

**Alternative (Go users):**

```bash
go install github.com/iimmutable/cc-modelrouter/cmd/ccrouter@latest
```

**From source:**

```bash
git clone https://github.com/iimmutable/cc-modelrouter
cd cc-modelrouter
make install
```

> **Note:** The first `ccrouter config` run downloads the latest provider presets from GitHub. To refresh presets later, delete `~/.cc-modelrouter/provider-presets.json` and re-run `ccrouter config`.
>
> **Install flags:** `bash install.sh --version vX.Y.Z -d /custom/path`

## Global Options

```
  -h, --help      Show help
  -v, --version   Show version
```

## Commands

### ccrouter code

Start the router and launch Claude Code.

```bash
ccrouter code [flags] [-- <claude-args>...]
```

**Flags:**
```
  -c, --config string          Path to config file
      --log-destination string Log destination (file|stdout|stderr|path)
      --log-level string       Log level: debug, info, warn, error
  -p, --port int               Port to listen on (default: 0 = OS picks a free port)
      --profile string         Route profile to use at startup
```

**Description:**
- Creates an isolated router instance
- Starts the HTTP server
- Launches Claude Code with `ANTHROPIC_BASE_URL` set to the router
- Creates a profile slash command for runtime profile switching
- Handles graceful shutdown on SIGINT/SIGTERM

**Permission Mode:**

By default, `ccrouter code` passes `--permission-mode auto` to Claude Code so you don't have to approve every tool call. This behavior can be controlled:

| Scenario | Behavior |
|----------|----------|
| `ccrouter code` | `--permission-mode auto` applied automatically |
| `ccrouter code --conservative` | No `--permission-mode` flag sent (uses Claude Code defaults) |
| `ccrouter code -- --permission-mode default` | Your explicit choice is respected |

**Argument Passthrough:**

Unknown flags are passed through to Claude Code. Use `--` to explicitly separate router flags from Claude Code flags:

```bash
# Pass model flag to Claude Code
ccrouter code -- --model claude-opus-4-6

# Mix router and Claude flags (unknown flags pass through)
ccrouter code --log-level=debug --model claude-sonnet-4-6
```

**Examples:**
```bash
# Use default or project config (auto permissions)
ccrouter code

# Use specific config file
ccrouter code -c /path/to/config.json

# Use specific port
ccrouter code -p 9090

# Enable debug logging to file
ccrouter code --log-level=debug --log-destination=file

# Use conservative (default) permissions
ccrouter code --conservative

# Pass a specific model to Claude Code
ccrouter code -- --model claude-opus-4-6
```

---

### ccrouter start

Start the router server standalone.

```bash
ccrouter start [flags]
```

**Flags:**
```
  -c, --config string          Path to config file
  -p, --port int               Port to listen on (overrides config)
  -H, --host string            Host to bind to (overrides config)
      --log-destination string Log destination (file|stdout|stderr|path)
      --log-level string       Log level: debug, info, warn, error
      --profile string         Route profile to use at startup
```

**Description:**
- Starts the HTTP server in the foreground
- Saves instance metadata for management
- Does NOT launch Claude Code

**Examples:**
```bash
# Start with default config
ccrouter start

# Start on specific port
ccrouter start -p 9090

# Use specific config
ccrouter start -c /path/to/config.json

# Start with debug logging to stdout
ccrouter start --log-level=debug --log-destination=stdout
```

---

### ccrouter stop

Stop a router instance.

```bash
ccrouter stop [instance-id] [flags]
```

**Arguments:**
```
  instance-id   ID of instance to stop (optional — stops all if omitted)
```

**Flags:**
```
  -f, --force   Force stop using SIGKILL instead of SIGTERM
```

**Description:**
- Stops the specified instance by PID
- Removes instance metadata file
- If no ID provided, stops all running instances

**Examples:**
```bash
# Stop specific instance
ccrouter stop inst_20250216_143022

# Stop all instances
ccrouter stop

# Force kill a stuck instance
ccrouter stop -f inst_20250216_143022
```

---

### ccrouter restart

Restart a router instance.

```bash
ccrouter restart [instance-id] [flags]
```

**Arguments:**
```
  instance-id   ID of instance to restart (optional — restarts all if omitted)
```

**Flags:**
```
  -c, --config string   Path to config file for restart
```

**Description:**
- Stops the instance
- Starts a new instance with the same configuration
- Reloads config from disk
- If no ID provided, restarts all running instances

**Examples:**
```bash
# Restart specific instance
ccrouter restart inst_20250216_143022

# Restart all instances
ccrouter restart
```

---

### ccrouter status

Show all running instances.

```bash
ccrouter status [flags]
```

**Flags:**
```
  -a, --all   Show all instances including dead ones
```

**Output:**
```
ID                      PORT    PID     CONFIG TYPE    STARTED
inst_20250216_143022    8081    12345   project        2025-02-16 14:30:22
inst_20250216_150033    8082    12346   global         2025-02-16 15:00:33
```

---

### ccrouter clean

Remove stale instance files.

```bash
ccrouter clean [flags]
```

**Flags:**
```
  -a, --all              Remove all instance files including running ones
                          Use with caution - this will remove metadata for active instances
      --usage-before <d> Prune usage records older than the given duration
                          Duration format: 30d (days), 24h (hours), 90m (minutes)
      --usage-all        Delete all usage records
```

**Examples:**
```bash
# Prune usage records older than 30 days
ccrouter clean --usage-before 30d

# Delete all usage records
ccrouter clean --usage-all

# Combined: prune old usage + clean stale instances
ccrouter clean --usage-before 30d
```

**Description:**
- Removes metadata files for instances that are no longer running
- Useful for cleanup after crashes or manual process termination
- Use `--all` with caution — stops and removes all instances
- Use `--usage-before` to prune old usage tracking data from the database
- Use `--usage-all` to completely reset usage tracking

---

### ccrouter config

Interactive configuration wizard (TUI).

```bash
ccrouter config
```

**Flags:**
```
      --shell-export    Print shell export commands (for eval)
```

**Description:**
- Launches a full-screen terminal UI for managing all configuration
- Menu-driven interface for providers, routes, server, and logging settings
- Provider presets with autocomplete (alicloud, anthropic, bigmodel, openrouter, openrouter-openai, openrouter-anthropic, gemini) — see [Provider Presets](#provider-presets) below
- Model autocomplete suggestions when adding providers
- Connection testing for providers
- View and export current configuration

**Wizard Menu:**
1. **Providers** — Add, edit, delete, and test API providers
2. **Routes** — Configure routing rules
3. **Server** — Set host and port
4. **Logging** — Configure log level and destination
5. **View Config** — Browse current configuration
6. **Save & Exit** — Write changes to disk

**Keyboard Shortcuts (within wizard):**
| Key | Action |
|-----|--------|
| `↑/↓` or `k/j` | Navigate |
| `Enter` | Select |
| `Tab` | Next field |
| `Esc` | Back / Cancel |
| `a` | Add provider |
| `Del` or `d` | Delete |

**Examples:**
```bash
# Launch the configuration wizard
ccrouter config
```

> **Note:** This replaces the old `show`, `path`, and `init` subcommands.

---

### ccrouter gen

Generate configuration files for Claude Code integration.

```bash
ccrouter gen settings [flags]
```

**Description:**
- Generates a Claude Code `settings.local.json` / `settings.json` that points Claude Code at the router proxy and disables attribution
- Interactive (TTY) by default: prompts for deployment type (**Local** vs **Public**)
- When **Public** is chosen, detects the server's public IPv4 via `api.ipify.org` (3s timeout); detection failure falls back to `localhost` (never fatal)
- Non-interactive (piped stdin): defaults to `localhost` with **no** network call
- `--url` or `--ip` skips the prompt entirely (scripting / offline-friendly)
- API key resolved from `--key` (direct) or `--user` (keystore lookup)

**Flag precedence** (first match wins): `--url` → `--ip` → non-TTY localhost default → TTY interactive prompt.

**Flags:**
```
      --url string        Full router URL (overrides prompt and detection)
      --ip string         Server IP (skips prompt and detection; offline-friendly)
  -p, --port int          Router port (default 8081)
      --user string       Username to look up API key from keystore
      --key string        API key directly (overrides --user)
  -o, --output string     Output file path (default: stdout)
```

**Examples:**
```bash
# Interactive: pick Local or Public (default Local)
ccrouter gen settings --user alice

# Generate with a key directly
ccrouter gen settings --key sk-ccr-abc123

# Scripting / offline: specify the IP explicitly, no prompt
ccrouter gen settings --ip 10.0.0.5 --port 8081 --user alice

# Full URL override, write to a project-local file
ccrouter gen settings --url http://myserver:8081 -o .claude/settings.local.json
```

---

### ccrouter profile

Manage route profiles for switching between different route configurations during a session.

```bash
ccrouter profile <subcommand> [flags]
```

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `list` | List all configured profiles |
| `switch <profile>` | Switch to a different profile |
| `status` | Show the currently active profile |

#### ccrouter profile list

List all configured route profiles.

```bash
ccrouter profile list [flags]
```

**Flags:**
```
      --from-config    List profiles from config file instead of running instance
      --instance       Instance ID to query (uses most recent if not specified)
```

**Description:**
- Shows all profiles with their names and descriptions
- Marks the active profile with `*`
- Can query from config file or running instance

**Examples:**
```bash
# List profiles from running instance
ccrouter profile list

# List profiles from config file
ccrouter profile list --from-config

# List profiles for specific instance
ccrouter profile list --instance inst_20250216_143022
```

#### ccrouter profile switch

Switch to a different route profile.

```bash
ccrouter profile switch <profile-name> [flags]
```

**Arguments:**
```
  profile-name   Name/key of the profile to switch to (required)
```

**Flags:**
```
      --instance   Instance ID to switch (uses most recent if not specified)
```

**Description:**
- Hot-swaps routes without restarting the router
- Requires a running router instance
- Updates instance metadata with new active profile

**Examples:**
```bash
# Switch to "cost-opt" profile
ccrouter profile switch cost-opt

# Switch for specific instance
ccrouter profile switch production --instance inst_20250216_143022
```

#### ccrouter profile status

Show the currently active profile.

```bash
ccrouter profile status [flags]
```

**Flags:**
```
      --instance   Instance ID to query (uses most recent if not specified)
```

**Description:**
- Shows the active profile name for a running instance
- Reports "No profiles configured" if using legacy routes

**Examples:**
```bash
# Show active profile
ccrouter profile status

# Show for specific instance
ccrouter profile status --instance inst_20250216_143022
```

---

### ccrouter logs

Show logs for an instance.

```bash
ccrouter logs [instance-id] [flags]
```

**Arguments:**
```
  instance-id   ID of instance (optional, shows all if not provided)
```

**Flags:**
```
  -f, --follow   Follow log output (like tail -f)
  -n, --tail int Number of lines to show from the end (default: 100)
```

**Examples:**
```bash
# Show all logs
ccrouter logs

# Show logs for specific instance
ccrouter logs inst_20250216_143022

# Follow logs in real-time
ccrouter logs -f inst_20250216_143022

# Show last 50 lines
ccrouter logs -n 50 inst_20250216_143022
```

---

### ccrouter keys

Manage API keys for multi-user mode.

```bash
ccrouter keys <subcommand> [flags]
```

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `create` | Create a new API key |
| `list` | List all API keys |
| `revoke <id>` | Revoke an API key |

Keys are stored in `~/.cc-modelrouter/usage.db`. A running server is not required.

#### ccrouter keys create

Create a new API key for a user. The full key is displayed once at creation time and cannot be retrieved later.

```bash
ccrouter keys create [flags]
```

**Flags:**
```
      --name string    Human-readable name for the key (required)
      --group string   Group to assign the key to (required)
```

**Description:**
- Generates a key with `sk-ccr-` prefix
- SHA-256 hash stored in SQLite (raw key never persisted)
- Returns the full key — save it immediately

**Examples:**
```bash
# Create a key for a developer
ccrouter keys create --name alice --group developers

# Create a key for an intern
ccrouter keys create --name "bob" --group interns
```

#### ccrouter keys list

List all API keys with metadata.

```bash
ccrouter keys list
```

**Output:**
```
ID    PREFIX          NAME     GROUP        ACTIVE  LAST USED
1     sk-cc-a1b2...   alice    developers   yes     2026-06-09 14:30
2     sk-cc-c3d4...   bob      interns      yes     (never)
```

#### ccrouter keys revoke

Revoke an API key by its database ID.

```bash
ccrouter keys revoke <id>
```

**Arguments:**
```
  id   Database ID of the key to revoke (from `ccrouter keys list`)
```

**Examples:**
```bash
ccrouter keys revoke 2
```

---

### ccrouter groups

Manage user groups for multi-user mode. Groups map API keys to routing profiles with QoS settings.

```bash
ccrouter groups <subcommand> [flags]
```

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `list` | List all groups with member counts |
| `create` | Create a new group |
| `update <id>` | Update group settings |
| `delete <id>` | Delete a group |

Groups are stored in `~/.cc-modelrouter/usage.db`. A running server is not required.

#### ccrouter groups create

Create a new user group with routing profile and QoS settings.

```bash
ccrouter groups create [flags]
```

**Flags:**
```
      --name string          Group name (required)
      --profile string       Routing profile to use (default: "")
      --priority float       Priority weight 0.0-1.0 (required)
      --max-concurrency int  Max concurrent requests for this group (0 = unlimited)
```

**Description:**
- `priority` determines guaranteed capacity share: `ceil(globalMax × priority)`
- Groups can borrow idle capacity from other groups when under-utilized
- `profile` maps to existing route profiles in config

**Examples:**
```bash
# Create a developers group with 70% priority
ccrouter groups create --name developers --profile standard --priority 0.7 --max-concurrency 50

# Create an interns group with 30% priority
ccrouter groups create --name interns --profile cost-opt --priority 0.3 --max-concurrency 10
```

#### ccrouter groups list

List all groups with member counts and settings.

```bash
ccrouter groups list
```

**Output:**
```
ID    NAME         PROFILE     PRIORITY  MAX CONC  MEMBERS
1     developers   standard    0.70      50        3
2     interns      cost-opt    0.30      10        1
```

#### ccrouter groups update

Update an existing group's settings.

```bash
ccrouter groups update <id> [flags]
```

**Arguments:**
```
  id   Database ID of the group (from `ccrouter groups list`)
```

**Flags:**
```
      --profile string       New routing profile
      --priority float      New priority weight
      --max-concurrency int New max concurrency
```

**Examples:**
```bash
# Update group priority
ccrouter groups update 1 --priority 0.8

# Change routing profile
ccrouter groups update 2 --profile standard
```

#### ccrouter groups delete

Delete a group. Fails if any API keys reference the group.

```bash
ccrouter groups delete <id>
```

**Arguments:**
```
  id   Database ID of the group (from `ccrouter groups list`)
```

**Examples:**
```bash
ccrouter groups delete 2
```

---

### ccrouter monitor

Live usage monitor with terminal UI.

```bash
ccrouter monitor [flags]
```

<!-- AUTO-GENERATED:START:monitor -->
**Flags:**
```
      --refresh duration   Stats refresh interval (default: 500ms)
```

**Description:**
- Displays a real-time dashboard with usage statistics
- Stats by route and model (requests, tokens, fallbacks)
- Date range selection: TODAY, WEEK, MONTH, YTD, TTM
- Instance filtering with running/stopped indicators
- Optional console log viewer (press `d` when single instance selected)

**Keyboard Shortcuts:**
| Key | Action |
|-----|--------|
| `q` | Quit |
| `d` | Toggle console log (single instance only) |
| `←` / `→` | Navigate date range tabs |
| `↑` / `↓` | Navigate instance list |
| `space` | Pause/resume log tail |
| `1-7` | Toggle log level filters |
| `r` | Force refresh |

**Examples:**
```bash
# Start monitor with default 500ms refresh
ccrouter monitor

# Start with custom refresh interval
ccrouter monitor --refresh 2s
```

<!-- AUTO-GENERATED:END:monitor -->

---

## Instance Management

### Instance Metadata

Instances are stored in `~/.cc-modelrouter/instances/`:

```json
{
  "id": "inst_20250216_143022",
  "port": 8081,
  "pid": 12345,
  "configType": "project",
  "configPath": "/path/to/project/.cc-modelrouter/config.json",
  "startTime": "2025-02-16T14:30:22Z",
  "projectRoot": "/path/to/project",
  "adminToken": "<generated-token>",
  "activeProfile": "default"
}
```

### Directory Structure

```
~/.cc-modelrouter/
├── config.json                    # Global configuration
└── instances/
    ├── inst_20250216_143022.json  # Instance metadata
    ├── inst_20250216_150033.json
    └── ...
```

## Typical Workflows

### Development (Project-Specific)

```bash
cd /path/to/project

# Create project config
mkdir -p .cc-modelrouter
cat > .cc-modelrouter/config.json << 'EOF'
{
  "server": {"port": 8081},
  "providers": {...},
  "router": {...}
}
EOF

# Start with project config
ccrouter code
```

### Multiple Projects

```bash
# Terminal 1: Project A
cd /path/to/project-a
ccrouter code    # Uses .cc-modelrouter/config.json

# Terminal 2: Project B
cd /path/to/project-b
ccrouter code    # Uses different config
```

### Standalone Server

```bash
# Start server
ccrouter start

# In another terminal, use with Claude Code
export ANTHROPIC_BASE_URL=http://localhost:8081
claude

# When done
ccrouter stop --all
```

## Environment Variables

The `ccrouter code` command automatically sets:

| Variable | Value |
|----------|-------|
| `ANTHROPIC_BASE_URL` | `http://<host>:<port>` |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Server startup error |

## Provider Presets

The configuration wizard offers provider presets (base URL, transformer, and
common models) for autocomplete when adding a provider. Presets are loaded
from JSON — not baked into the binary.

### Auto-fetch on first run

When you run `ccrouter config` and `~/.cc-modelrouter/provider-presets.json`
does not exist, the wizard fetches the canonical presets from GitHub:

```
https://raw.githubusercontent.com/iimmutable/cc-modelrouter/master/presets/provider-presets.json
```

The file is written to `~/.cc-modelrouter/provider-presets.json` with `0600`
permissions. On the next run the file already exists, so no network access
is needed.

To refresh presets later:

```bash
rm ~/.cc-modelrouter/provider-presets.json
ccrouter config
```

### Preset layering

Presets are merged from three layers (later layers override per key):

| Layer | Path | Notes |
|-------|------|-------|
| Fetched defaults | `~/.cc-modelrouter/provider-presets.json` | Written on first run |
| Global override | `~/.cc-modelrouter/provider-presets.json` | Same path (you can hand-edit) |
| Project override | `<cwd>/.cc-modelrouter/provider-presets.json` | Per-project presets |

Each provider key in a JSON file fully replaces the inherited entry for that
key. Missing keys keep their inherited value.

### File format

```json
{
  "anthropic": {
    "baseUrl": "https://api.anthropic.com",
    "transformer": "anthropic",
    "models": ["claude-haiku-4.5", "claude-sonnet-4.6"]
  },
  "my-custom": {
    "baseUrl": "https://internal.example.com",
    "transformer": "anthropic",
    "models": ["custom-model-1"]
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `baseUrl` | string | Provider API base URL |
| `transformer` | string | One of: `anthropic`, `openai`, `glm_anthropic`, `gemini` |
| `models` | string[] | Suggested model names for autocomplete |

### Offline behavior

If the fetch fails (offline, GitHub down, invalid response), the wizard
launches with an empty preset set. You can still add providers manually.
The next `ccrouter config` run retries the fetch automatically.
