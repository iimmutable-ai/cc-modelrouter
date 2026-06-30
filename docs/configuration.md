# Configuration Guide

## Configuration Files

### Locations

| Scope | Path | Priority |
|-------|------|----------|
| Global | `~/.cc-modelrouter/config.json` | Low |
| Project | `<project>/.cc-modelrouter/config.json` | High (overrides global) |
| Global presets | `~/.cc-modelrouter/provider-presets.json` | Wizard autocomplete (fetched on first run) |
| Project presets | `<project>/.cc-modelrouter/provider-presets.json` | Wizard autocomplete (overrides global presets) |

Project configuration **completely overrides** global configuration when present. There is no deep merging.

> **Provider presets** are a separate JSON file used only by the `ccrouter config` wizard for autocomplete suggestions (provider base URL, transformer, and common models). They are auto-fetched from GitHub on the first `ccrouter config` run. See [CLI Reference - Provider Presets](cli-reference.md#provider-presets) for details.

### Basic Structure

```json
{
  "server": {
    "port": 8081,
    "host": "localhost"
  },
  "providers": {
    "provider-name": {
      "apiKey": "your-api-key",
      "baseURL": "https://api.example.com",
      "models": ["model-1", "model-2"]
    }
  },
  "router": {
    "routes": {
      "default": "provider:model",
      "background": "provider:model"
    },
    "profiles": {
      "default": {
        "name": "Default",
        "routes": { "default": "provider:model" }
      },
      "cost-opt": {
        "name": "Cost Optimized",
        "description": "Use cheaper models",
        "routes": { "default": "provider:cheap-model" }
      }
    },
    "maxRetries": 2,
    "retryDelay": "500ms"
  },
  "logging": {
    "enabled": false,
    "destination": "file",
    "level": "info"
  }
}
```

> **Note:** Top-level `"profiles"` is also supported for backward compatibility and is automatically migrated to `router.profiles` at load time.

## Server Configuration

```json
{
  "server": {
    "port": 8081,
    "host": "localhost",
    "autoRestartIdle": "30m",
    "autoRestartWindow": "03:00-05:00",
    "autoRestartTimezone": "Asia/Shanghai",
    "autoRestartBackoffMax": "10m",
    "userAgent": ""
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | 8081 | Port to listen on |
| `host` | string | localhost | Host to bind to |
| `autoRestartIdle` | duration string | `""` (disabled) | Self-restart after the server is idle (no requests, zero in-flight connections) for this long. Examples: `30m`, `2h`. Empty disables auto-restart. |
| `autoRestartWindow` | `HH:MM-HH:MM` | `""` (always) | Restrict restart eligibility to a time-of-day window (24h, strict 2-digit hour and minute). Supports overnight wrap (e.g. `23:00-04:00`). Interpreted in `autoRestartTimezone`. Empty = always eligible. |
| `autoRestartTimezone` | IANA name | `""` (Local) | Timezone used to evaluate `autoRestartWindow` (e.g. `Asia/Shanghai`, `UTC`). Empty = server local time. |
| `autoRestartBackoffMax` | duration string | `""` (none) | Cap on a random jitter delay applied after idle fires and before the restart, to desynchronize multiple instances. Empty/`0` = no backoff. |
| `userAgent` | string | `""` (Claude Code SDK UA) | Overrides the `User-Agent` header sent to providers. Empty = the default that mimics the `@anthropic-ai/sdk` User-Agent Claude Code sends (`@anthropic-ai/sdk/0.30.0 bun/1.3.13 darwin/arm64`). |

### Auto-Restart

The router can restart itself after a sustained idle period, optionally constrained to a time-of-day window. This is useful for recycling long-running daemons to release OS resources or pick up environment changes without interrupting active traffic.

- **Idle gate**: the watcher only fires when there are no in-flight connections and no recent requests for the full `autoRestartIdle` duration.
- **Window gate**: if `autoRestartWindow` is set, the restart only fires inside that window (evaluated in `autoRestartTimezone`).
- **Backoff**: when `autoRestartBackoffMax` is set, a random delay between `0` and the configured cap is applied before the restart, so multiple instances behind a fleet don't restart simultaneously.
- **Restart mechanism**: the new process replaces the old one in-place via `syscall.Exec`, inheriting the listening socket so clients see no dropped connections.

#### Restart outcome log

Auto-restart events are appended to `~/.cc-modelrouter/restarts.jsonl` (one JSON object per line):

| Event | Written when |
|-------|--------------|
| `initiated` | Before `syscall.Exec`, in the old process. |
| `restarted` | On boot, if the new process was spawned with `CCRROUTER_RESTART_FROM` carrying the old instance ID. |
| `exec_failed` | When `syscall.Exec` errors, in the old process, just before `os.Exit(1)`. |

The absence of a `restarted` record after an `initiated` record means the new process did not come up — a useful failure signal when monitoring long-running instances.

### User-Agent

By default the router sends the same `User-Agent` header Claude Code sends to Anthropic-protocol providers, so provider front-ends (e.g. GLM's `/api/anthropic`) see traffic as if it originated from Claude Code itself. Set `server.userAgent` to override for all providers; whitespace-only values are treated as unset.

## Provider Configuration

```json
{
  "providers": {
    "openrouter-anthropic": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
      "baseURL": "https://openrouter.ai/api",
      "transformer": "openrouter",
      "models": [
        "anthropic/claude-haiku-4.5",
        "anthropic/claude-sonnet-4.5",
        "anthropic/claude-opus-4.5"
      ]
    }
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiKey` | string | Yes | API key (supports env vars) |
| `baseURL` | string | Yes | API base URL |
| `models` | []string | Yes | List of available models |
| `transformer` | string | No | Transformer name (defaults to provider name) |
| `disableKeepAlives` | bool | No | Disable HTTP keep-alive connections (default: `false`). Use for providers with connection issues. |
| `maxRequestBodyBytes` | int64 | No | Maximum request body size in bytes (default: `0` = no limit). |
| `compaction` | object | No | Request compaction settings (see below). |

### Request Compaction

<!-- AUTO-GENERATED:START:compaction -->
Providers may have context window limits. Request compaction reduces large requests to fit within provider constraints.

```json
{
  "bigmodel": {
    "apiKey": "${CCROUTER_BIGMODEL_API_KEY}",
    "baseURL": "https://open.bigmodel.cn/api/anthropic",
    "models": ["glm-4.7"],
    "compaction": {
      "method": "llm",
      "summarizeProvider": "openrouter:anthropic/claude-sonnet-4",
      "summarizeModel": ""
    }
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | `"llm"` | Compaction method: `"llm"` (summarize via provider) or `"trim"` (truncate messages) |
| `summarizeProvider` | string | auto-detect | Provider for LLM summarization (format: `"provider:model"`) |
| `summarizeModel` | string | provider default | Model override for summarization |

<!-- AUTO-GENERATED:END:compaction -->

### Supported Providers

#### OpenRouter

OpenRouter provides a **unified Anthropic-compatible API** for all models (Claude, Gemini, OpenAI, etc.). The `openrouter` transformer handles signature preservation required by OpenRouter's validation.

**Provider Configuration:**

```json
{
  "openrouter-anthropic": {
    "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
    "baseURL": "https://openrouter.ai/api",
    "transformer": "openrouter",
    "models": ["anthropic/claude-haiku-4.5", "anthropic/claude-sonnet-4.5", "anthropic/claude-opus-4.5"]
  }
}
```

- **Endpoint**: `https://openrouter.ai/api` + `/v1/messages`
- **Transformer**: `openrouter` (preserves signature fields for thinking blocks)
- **Auth**: `x-api-key: <key>`
- **Supported Models**: Anthropic Claude models (`anthropic/*`)
- **Purpose**: Claude models with extended thinking support

**For non-Anthropic models** (Google Gemini, OpenAI, etc.):

```json
{
  "openrouter-openai": {
    "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
    "baseURL": "https://openrouter.ai/api",
    "transformer": "openrouter",
    "models": ["google/gemini-2.5-flash", "google/gemini-2.5-pro"]
  }
}
```

- **Endpoint**: `https://openrouter.ai/api` + `/v1/messages` (same as above)
- **Transformer**: `openrouter` (same as above)
- **Auth**: `x-api-key: <key>` (same as above)
- **Supported Models**: Google, OpenAI, and other models
- **Purpose**: Non-Anthropic models (logical separation only)

**Note**: The provider split (`openrouter-anthropic` vs `openrouter-openai`) is for **logical organization** only. Both use the same API endpoint and transformer. The split allows you to group models by type in your routes, but there's no technical difference in how they're handled.

**Why `openrouter` transformer?**
- OpenRouter's API requires the `signature` field to be present in thinking blocks (even when empty)
- The `anthropic` transformer strips empty signatures, causing 400 errors
- The `openrouter` transformer preserves signatures by setting them to `" "`

**Using a single provider alternative:**
If you prefer, you can combine all OpenRouter models into a single provider:
```json
{
  "openrouter": {
    "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
    "baseURL": "https://openrouter.ai/api",
    "transformer": "openrouter",
    "models": [
      "anthropic/claude-haiku-4.5",
      "anthropic/claude-sonnet-4.6",
      "anthropic/claude-opus-4.5",
      "google/gemini-2.5-flash",
      "google/gemini-2.5-pro"
    ]
  }
}
```

#### Google Gemini

```json
{
  "gemini": {
    "apiKey": "${CCROUTER_GEMINI_API_KEY}",
    "baseURL": "https://generativelanguage.googleapis.com/v1beta",
    "models": ["gemini-2.0-flash", "gemini-2.5-pro"]
  }
}
```

- **Transformer**: `gemini` (native format)
- **Auth**: Query parameter `key=<api-key>`

#### Aliyun DashScope (alicloud)

```json
{
  "alicloud": {
    "apiKey": "${CCROUTER_DASHSCOPE_API_KEY}",
    "baseURL": "https://coding.dashscope.aliyuncs.com/apps/anthropic",
    "transformer": "glm_anthropic",
    "models": ["MiniMax-M2.5", "kimi-k2.5", "qwen3-coder-plus", "glm-5", "glm-4.7", "qwen3.7-plus", "glm-5.1", "glm-5.2", "kimi-k2.6"]
  }
}
```

- **Transformer**: `glm_anthropic` (Anthropic-compatible)
- **Auth**: `Authorization: Bearer`
- **Note**: This preset is auto-suggested by `ccrouter config` when you name a provider `alicloud`.

#### Zhipu GLM (BigModel)

```json
{
  "bigmodel": {
    "apiKey": "${CCROUTER_BIGMODEL_API_KEY}",
    "baseURL": "https://open.bigmodel.cn/api/anthropic",
    "models": ["glm-4.6v", "glm-4.7", "glm-5-turbo", "glm-5v-turbo", "glm-5.1", "glm-5.2"]
  }
}
```

- **Transformer**: `glm-anthropic` (Anthropic-compatible)
- **Auth**: `x-api-key: <key>`

#### Anthropic (Direct)

```json
{
  "anthropic": {
    "apiKey": "${CCROUTER_ANTHROPIC_API_KEY}",
    "baseURL": "https://api.anthropic.com",
    "models": ["claude-haiku-4.5", "claude-sonnet-4.6", "claude-opus-4.5", "claude-opus-4.6"]
  }
}
```

- **Transformer**: `anthropic` (pass-through)
- **Auth**: `x-api-key: <key>`

## Router Configuration

```json
{
  "router": {
    "routes": {
      "default": "openrouter:anthropic/claude-sonnet-4.6",
      "background": "bigmodel:glm-5-turbo",
      "think": "openrouter:anthropic/claude-sonnet-4.6",
      "thinkMore": "openrouter:anthropic/claude-sonnet-4.6",
      "ultrathink": "openrouter:anthropic/claude-opus-4.6",
      "longContext": "gemini:gemini-2.5-pro",
      "webSearch": "gemini:gemini-2.5-pro",
      "image": "bigmodel:glm-4.6v"
    },
    "maxRetries": 2,
    "retryDelay": "500ms"
  }
}
```

### Routes (priority order — checked top to bottom)

| Priority | Route | Description | Trigger | Detection Method |
|----------|-------|-------------|---------|------------------|
| 1 | `background` | Background tasks | Background agent request | Model name contains both `claude` and `haiku` (case-insensitive) |
| 2 | `subagent` | Subagent tasks | Subagent dispatch (Claude Code v2.1.139+) | HTTP headers `X-Claude-Code-Agent-Id` / `X-Claude-Code-Parent-Agent-Id`; fallback: tool name contains "subagent", or last user message contains "subagent"/"delegate to agent" |
| 3 | `review` | Review tasks | Review keyword in last user message | Last user message contains "/review", "code review", "review this", "review the", or starts with "review " |
| 4 | `ultrathink` | Maximum thinking | "ultrathink", "think harder" | `budget_tokens >= 32,000` |
| 5 | `thinkMore` | Enhanced thinking | "think hard", "think more" | `budget_tokens >= 10,000` |
| 6 | `think` | Basic thinking | "think" trigger phrase | `budget_tokens >= 4,000` |
| 7 | `image` | Image processing | Images in request | Request contains image blocks |
| 8 | `webSearch` | Web search enabled | Web search tools | Tool names contain "web"/"search" |
| 9 | `longContext` | Long conversations | Large context | Token count > 60,000 |
| 10 | `default` | Default fallback | All unmatched requests | - |

### Thinking Levels

Claude Code supports multiple thinking intensity levels. When a user types trigger phrases like "think", "think more", or "ultrathink", Claude Code converts these to specific `budget_tokens` values before sending the API request.

| Level | Budget Tokens | Route | Trigger Phrases |
|-------|---------------|-------|-----------------|
| Basic | ~4,000 | `think` | "think", "思考" |
| Middle | ~10,000 | `thinkMore` | "think hard", "think more", "think deeply", "megathink", "好好想", "多想想" |
| Highest | ~32,000 | `ultrathink` | "ultrathink", "think harder", "think intensely", "think longer", "仔细思考", "深思" |

**Fallback Behavior:**

The router supports flexible thinking configuration with automatic fallback:

1. **Full 3-tier config:** Configure `think`, `thinkMore`, and `ultrathink` for different models at each level
2. **2-tier config:** Configure only `think` and `thinkMore` - highest level uses `thinkMore`
3. **1-tier config:** Configure only `think` - all thinking levels use `think`

Example for cost optimization:
```json
{
  "router": {
    "routes": {
      "default": "openrouter:claude-sonnet-4",
      "think": "openrouter:claude-sonnet-4",
      "thinkMore": "openrouter:claude-sonnet-4",
      "ultrathink": "openrouter:claude-opus-4"
    }
  }
}
```

### Route Format

```
provider:model[;provider:model;...]
```

Multiple targets are tried in sequence with failover:

```json
{
  "default": "openrouter:claude-sonnet-4;bigmodel:glm-4.7;gemini:gemini-2.5-pro"
}
```

### Retry Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `maxRetries` | int | 2 | Max retries per route |
| `retryDelay` | string | 500ms | Delay between retries |

## Route Profiles

<!-- AUTO-GENERATED:START:profiles -->
Route profiles allow you to define multiple route configurations and switch between them during a session without restarting the router.

### Profile Configuration

Profiles are stored under `router.profiles` in the config file. Top-level `"profiles"` is also accepted for backward compatibility and is automatically migrated during loading.

```json
{
  "router": {
    "profiles": {
      "default": {
        "name": "Default",
        "description": "Standard routing with Claude models",
        "routes": {
          "default": "openrouter:anthropic/claude-sonnet-4",
          "background": "bigmodel:glm-5-turbo",
          "think": "openrouter:anthropic/claude-sonnet-4",
          "ultrathink": "openrouter:anthropic/claude-opus-4"
        }
      },
      "cost-opt": {
        "name": "Cost Optimized",
        "description": "Use cheaper models for cost savings",
        "routes": {
          "default": "bigmodel:glm-4.7",
          "background": "bigmodel:glm-5-turbo",
          "think": "bigmodel:glm-4.7",
          "ultrathink": "openrouter:anthropic/claude-sonnet-4"
        }
      },
      "premium": {
        "name": "Premium",
        "description": "Maximum quality with Claude Opus",
        "routes": {
          "default": "openrouter:anthropic/claude-opus-4",
          "background": "openrouter:anthropic/claude-haiku-4.5",
          "think": "openrouter:anthropic/claude-opus-4",
          "ultrathink": "openrouter:anthropic/claude-opus-4"
        }
      }
    }
  }
}
```

### Profile Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Display name for the profile |
| `description` | string | No | Optional description of the profile purpose |
| `routes` | object | Yes | Route name to `provider:model` chain mapping |

### Default Profile Behavior

When the router starts, it automatically selects the default profile:
- If a profile named "default" exists, it is used
- Otherwise, the first profile alphabetically is used
- If no profiles are configured, legacy `router.routes` are used

The profile selection is **runtime-only** — it is not persisted to the config file. Switching profiles during a session affects only the current running instance, not the configuration file.

### Legacy Compatibility

If `profiles` is empty or not configured, the router falls back to `router.routes` for backward compatibility with existing configurations.

### Switching Profiles

Use the CLI to switch profiles during a running session:

```bash
# List available profiles
ccrouter profile list

# Switch to a different profile
ccrouter profile switch cost-opt

# Show current active profile
ccrouter profile status
```

Profile switching is hot-swappable — no restart required. The router immediately applies the new routes for subsequent requests.
<!-- AUTO-GENERATED:END:profiles -->

## Logging Configuration

```json
{
  "logging": {
    "enabled": true,
    "destination": "file",
    "filePath": "~/.cc-modelrouter/router.log",
    "level": "info"
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable logging (opt-in) |
| `destination` | string | - | `stdout`, `stderr`, `file`, or a custom file path |
| `filePath` | string | - | Override default log file path |
| `level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |

**Default log paths (when destination is `file` or empty):**
- Without instance: `~/.cc-modelrouter/router.log`
- With instance: `~/.cc-modelrouter/logs/<instanceID>.log`

**Log levels** (most to least verbose):
| Level | Description |
|-------|-------------|
| `debug` | Full request/response details, headers (sanitized), SSE events |
| `info` | Route detection, provider selection, request summary |
| `warn` | Retries, non-fatal errors, filtered events |
| `error` | Provider errors, transform failures, startup errors |

**CLI override:** `--log-level` and `--log-destination` flags override config settings.

## Environment Variables

Use `${VAR_NAME}` or `$VAR_NAME` syntax for secure value injection:

```json
{
  "providers": {
    "openrouter-anthropic": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
      "baseURL": "https://openrouter.ai/api",
      "transformer": "openrouter"
    }
  }
}
```

### Setting Environment Variables

```bash
# In ~/.bashrc or ~/.zshrc
export CCROUTER_OPENROUTER_API_KEY="sk-or-..."
export CCROUTER_GEMINI_API_KEY="AIza..."
export CCROUTER_BIGMODEL_API_KEY="..."
```

## Multi-User Configuration

Multi-user mode enables team sharing of a single router instance with API key authentication, per-group routing profiles, and QoS with priority queuing.

### Enabling Multi-User

Add the `multiUser` section to your config:

```json
{
  "multiUser": {
    "enabled": true,
    "globalMaxConcurrency": 100,
    "groups": [
      {
        "name": "developers",
        "profile": "standard",
        "priorityWeight": 0.7,
        "maxConcurrency": 50
      },
      {
        "name": "interns",
        "profile": "cost-opt",
        "priorityWeight": 0.3,
        "maxConcurrency": 10
      }
    ]
  }
}
```

When `multiUser.enabled` is `false` or absent, the server operates in single-user mode — no authentication, no QoS, no interceptors.

### MultiUser Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable multi-user mode |
| `globalMaxConcurrency` | int | 100 | Maximum concurrent requests across all groups |
| `wredMinDepth` | float | 0.5 | WRED drop probability starts at this queue depth fraction |
| `wredMaxDepth` | float | 0.9 | WRED drop probability reaches 100% at this queue depth fraction |
| `groups` | []GroupConfig | [] | User group definitions |

### GroupConfig Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique group name |
| `profile` | string | No | Route profile to use (empty = active profile) |
| `priorityWeight` | float | Yes | Priority weight 0.0-1.0 (guaranteed share = `ceil(globalMax × weight)`) |
| `maxConcurrency` | int | No | Max concurrent requests for this group (0 = unlimited) |

### Key Concepts

| Concept | Description |
|---------|-------------|
| **API Keys** | `sk-ccr-...` bearer tokens, set as `ANTHROPIC_API_KEY` in Claude Code |
| **User Groups** | Map keys to routing profiles with QoS settings |
| **Guaranteed Shares** | `ceil(globalMax × priorityWeight)` — guaranteed capacity per group |
| **Idle Borrowing** | Groups can borrow unused capacity from other groups |
| **WRED** | Weighted Random Early Detection drops requests when queue is full |
| **Provider AIMD** | Auto-detects 429s and adjusts concurrency limits (additive increase, multiplicative decrease) |

### Managing Keys and Groups

API keys and groups are stored in SQLite (`~/.cc-modelrouter/usage.db`), not in config.json. Use the CLI to manage them:

```bash
# Create a group
ccrouter groups create --name developers --profile standard --priority 0.7 --max-concurrency 50

# Create an API key (save the key — it's shown only once)
ccrouter keys create --name alice --group developers

# List keys and groups
ccrouter keys list
ccrouter groups list

# Revoke a key
ccrouter keys revoke <id>

# Delete a group (fails if keys reference it)
ccrouter groups delete <id>
```

### Using Multi-User Mode

1. Configure `multiUser` in config with at least one group
2. Start the server: `ccrouter start`
3. Create API keys: `ccrouter keys create --name <name> --group <group>`
4. Configure Claude Code to use the router with the API key:
   ```bash
   export ANTHROPIC_BASE_URL=http://localhost:8081
   export ANTHROPIC_API_KEY=sk-ccr-...
   claude
   ```

## Complete Example

```json
{
  "server": {
    "port": 8081,
    "host": "localhost"
  },
  "providers": {
    "bigmodel": {
      "apiKey": "${CCROUTER_BIGMODEL_API_KEY}",
      "baseURL": "https://open.bigmodel.cn/api/anthropic",
      "models": ["glm-4.6v", "glm-4.7", "glm-5-turbo", "glm-5v-turbo", "glm-5.1", "glm-5.2"],
      "transformer": "glm_anthropic"
    },
    "openrouter-anthropic": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
      "baseURL": "https://openrouter.ai/api",
      "models": ["anthropic/claude-haiku-4.5", "anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.5"],
      "transformer": "openrouter"
    },
    "openrouter-openai": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}",
      "baseURL": "https://openrouter.ai/api",
      "models": ["google/gemini-2.5-flash", "google/gemini-2.5-pro"],
      "transformer": "openrouter"
    },
    "gemini": {
      "apiKey": "${CCROUTER_GEMINI_API_KEY}",
      "baseURL": "https://generativelanguage.googleapis.com/v1beta",
      "models": ["gemini-2.5-pro", "gemini-2.0-flash"]
    },
    "alicloud": {
      "apiKey": "${CCROUTER_DASHSCOPE_API_KEY}",
      "baseURL": "https://coding.dashscope.aliyuncs.com/apps/anthropic",
      "models": ["MiniMax-M2.5", "kimi-k2.5", "qwen3-coder-plus", "glm-5", "glm-4.7", "qwen3.7-plus", "glm-5.1", "glm-5.2", "kimi-k2.6"],
      "transformer": "glm_anthropic"
    }
  },
  "router": {
    "routes": {
      "default": "bigmodel:glm-4.7;alicloud:glm-4.7;openrouter-anthropic:anthropic/claude-sonnet-4.6",
      "background": "bigmodel:glm-5-turbo;alicloud:glm-5;openrouter-openai:google/gemini-2.5-flash;openrouter-anthropic:anthropic/claude-haiku-4.5",
      "think": "bigmodel:glm-4.7;alicloud:glm-4.7;openrouter-anthropic:anthropic/claude-sonnet-4.6",
      "thinkMore": "alicloud:glm-5;openrouter-anthropic:anthropic/claude-opus-4.5",
      "longContext": "alicloud:glm-5;openrouter-openai:google/gemini-2.5-pro"
    },
    "maxRetries": 2,
    "retryDelay": "500ms"
  }
}
```

## Project-Level Override

For project-specific configuration, create `.cc-modelrouter/config.json` in your project root:

```
my-project/
├── .cc-modelrouter/
│   └── config.json    # Project-specific config
├── src/
└── ...
```

When running `ccrouter code` from within the project directory, the project config will be used instead of the global config.

## Viewing Active Configuration

```bash
ccrouter config
```

This launches an interactive configuration wizard where you can view, edit, and export the current configuration. Select "View Config" from the main menu to browse the active configuration (global or project-level).
