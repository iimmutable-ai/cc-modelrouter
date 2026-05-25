# CLI Reference

## Installation

```bash
go install github.com/iimmutable/cc-modelrouter/cmd/ccrouter@latest
```

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
- Provider presets with autocomplete (alicloud, anthropic, bigmodel, openrouter, openrouter-openai, openrouter-anthropic)
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
