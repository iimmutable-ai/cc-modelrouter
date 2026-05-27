# Architecture Overview

cc-modelrouter is built as a layered architecture with clear separation of concerns.

## Layered Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ CLI / Config Layer                                          │
│ (start/stop/status, config loading/merging)                 │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│ HTTP Server Layer                                           │
│ (request validation, Anthropic API endpoint)                │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│ Router Engine Layer                                         │
│ (route matching, failover logic, model selection)           │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│ Transformer Layer                                           │
│ (anthropic, openai, openrouter, gemini, glm)                │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│ Provider Client Layer                                       │
│ (HTTP clients, streaming, retry)                            │
└─────────────────────────────────────────────────────────────┘
```

## Component Overview

### CLI Layer (`internal/cli/`)

Handles command-line interface using Cobra framework.

**Commands:**
- `code` - Start router and launch Claude Code
- `start` - Start router server standalone
- `stop` - Stop router instance
- `restart` - Restart instance
- `status` - Show running instances
- `clean` - Remove stale instance files
- `config` - Interactive TUI configuration wizard
- `profile` - Manage route profiles (list, switch, status)
- `logs` - Show instance logs
- `monitor` - Live usage monitor with terminal UI

### Configuration Layer (`internal/config/`)

Manages configuration loading with support for:
- Global configuration (`~/.cc-modelrouter/config.json`)
- Project configuration (`<project>/.cc-modelrouter/config.json`)
- Environment variable interpolation (`${VAR_NAME}` syntax)
- Configuration validation

**Key Types:**
```go
type Config struct {
    Server    ServerConfig
    Providers map[string]ProviderConfig
    Router    RouterConfig    // includes Routes, Profiles, MaxRetries, RetryDelay
    Logging   LoggingConfig   // opt-in logging
}
```

### Proxy Server Layer (`internal/proxy/`)

HTTP server implementing the Anthropic Messages API endpoint.

**Features:**
- Request validation (max 50MB)
- SSE streaming support
- Integration adapters for router and transformer layers

**Key Components:**
- `Server` - HTTP server with graceful shutdown
- `Handler` - Request handler for `/v1/messages`
- `AdminHandler` - Runtime profile management API (localhost-only, token-authenticated)
- `Compactor` - Request compaction for providers with context window limits
- `SSEWriter` - Server-Sent Events streaming

**Admin API Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/_admin/profiles` | List all configured profiles |
| `GET` | `/_admin/profiles/active` | Get the currently active profile |
| `POST` | `/_admin/profiles/switch` | Switch to a different profile |

All endpoints require `localhost` access and a valid `X-Admin-Token` header (or `?token=` query parameter). The admin token is generated at instance startup and stored in instance metadata.

### Router Engine Layer (`internal/router/`)

Determines which provider and model to use for each request.

**Route Detection (priority order — checked top to bottom):**
| Priority | Route | Trigger Condition | Detection Method |
|----------|-------|-------------------|------------------|
| 1 | `background` | Background agent | `IsBackground` flag on request |
| 2 | `subagent` | Subagent task | Prompt contains "subagent"/"delegate to agent" |
| 3 | `review` | Review task | Prompt contains "/review", "code review", or starts with "review " |
| 4 | `ultrathink` | Highest thinking | `budget_tokens >= 32,000` |
| 5 | `thinkMore` | Middle thinking | `budget_tokens >= 10,000` |
| 6 | `think` | Basic thinking | `budget_tokens >= 4,000` |
| 7 | `image` | Image content | Request contains image blocks |
| 8 | `webSearch` | Web search enabled | Tool names contain "web"/"search" |
| 9 | `longContext` | Large context | Token count > 60,000 |
| 10 | `default` | Fallback | All other requests |

**Thinking Levels:**

Claude Code converts trigger phrases to numeric `budget_tokens` before sending requests. Our router detects these levels:

| Level | Budget Threshold | Trigger Phrases |
|-------|-----------------|-----------------|
| `ThinkNone` | 0 | (no thinking) |
| `ThinkBasic` | >= 4,000 | "think", "思考" |
| `ThinkMiddle` | >= 10,000 | "think hard", "think more", "megathink" |
| `ThinkHighest` | >= 32,000 | "ultrathink", "think harder" |

**Thinking Route Fallback:**
- `ultrathink` → `thinkMore` → `think` (if not configured)
- `thinkMore` → `think` (if not configured)

**Failover Strategy:**
- Sequential: Try each provider:model in order
- Looping: After list exhausted, restart from beginning
- Max Attempts: 2 × number of providers in route

### Transformer Layer (`internal/transformer/`)

Converts requests and responses between Anthropic format and provider-specific formats.

**Interface:**
```go
type Transformer interface {
    Name() string
    Endpoint() string
    PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error)
    ParseResponse(resp *http.Response) (*anthropic.Response, error)
    SupportsStreaming() bool
    TransformStreamEvent(event *SSEEvent) ([]SSEEvent, error)
}
```

**Built-in Transformers:**

| Transformer | Format | Authentication |
|-------------|--------|----------------|
| `anthropic` | Anthropic native | `x-api-key` header |
| `openai` | OpenAI-compatible | `Authorization: Bearer` |
| `openrouter` | Anthropic native (with signature/thinking normalization) | `x-api-key` header |
| `gemini` | Gemini native (`contents`/`parts`) | Query param `key=` |
| `glm_anthropic` | Anthropic-compatible | `x-api-key` header |

### Provider Client Layer (`internal/provider/`)

HTTP client for communicating with provider APIs.

**Features:**
- Connection pooling
- Retry with configurable delay
- Streaming support
- Timeout handling

### Interceptor Layer (`internal/interceptor/`)

Cross-cutting request/response modifications applied at three points in the request pipeline.

**Built-in Interceptors:**

| Interceptor | Type | Description |
|-------------|------|-------------|
| `MaxTokensInterceptor` | Request | Adjusts `max_tokens` based on provider/model limits (e.g., OpenAI 4096, Claude 8192) |
| `ReasoningInterceptor` | Response/Streaming | Extracts and formats thinking/reasoning content for display |
| `ToolEnhanceInterceptor` | Request | Ensures tools have descriptions, normalizes parameter schemas |

**Registration points** (in `internal/proxy/`):
- `AddRequestInterceptor(fn)` — before routing
- `AddResponseInterceptor(fn)` — after provider response
- `AddStreamingInterceptor(fn)` — per SSE event

### Daemon Management (`internal/daemon/`)

Manages router instance lifecycle.

**Features:**
- Instance ID generation
- PID file management
- Instance metadata storage
- Dynamic port allocation

## Request Flow

```
Claude Code                    cc-modelrouter                    Provider
    │                               │                               │
    │  POST /v1/messages            │                               │
    │──────────────────────────────>│                               │
    │                               │                               │
    │                    Validate request                            │
    │                               │                               │
    │                    Detect route type                          │
    │                               │                               │
    │                    Select provider:model                       │
    │                               │                               │
    │                    Transform request                           │
    │                               │                               │
    │                               │  POST /chat/completions        │
    │                               │──────────────────────────────>│
    │                               │                               │
    │                               │                    Process request
    │                               │                               │
    │                               │  Response (or streaming)       │
    │                               │<──────────────────────────────│
    │                               │                               │
    │                    Transform response                          │
    │                               │                               │
    │  Response (or streaming)      │                               │
    │<──────────────────────────────│                               │
    │                               │                               │
```

## Instance Isolation

Each `ccrouter code` command creates an isolated environment:

```
~/.cc-modelrouter/
├── config.json                    # Global configuration
└── instances/
    ├── inst_20250216_143022.json  # Instance metadata
    ├── inst_20250216_150033.json
    └── ...
```

**Instance Metadata:**
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

## Design Decisions

### Why Go?

- Single binary distribution
- Excellent HTTP server performance
- Strong typing for API contracts
- Built-in concurrency support

### Why Registry Pattern for Transformers?

- Easy to add new transformers
- Runtime transformer selection
- Clean separation of concerns

### Why Instance Isolation?

- Multiple projects can run simultaneously
- Each project uses its own configuration
- No port conflicts with dynamic allocation

### Why Sequential Failover?

- Simple and predictable behavior
- Easy to reason about retry logic
- Matches common load balancer patterns

## Security Layer

### Logging Sanitization (`internal/logging/sanitize.go`)

Prevents API keys and secrets from leaking into log files.

**Features:**
- Automatic header redaction for sensitive values
- Case-insensitive header matching
- Preserves header names for debugging while hiding values
- Integrates with all logging functions

**Sensitive Headers Protected:**
- `Authorization` (Bearer tokens)
- `X-Api-Key` (API keys)
- `Cookie` / `Set-Cookie` (session data)
- Provider-specific headers (`X-BigModel-Api-Key`, etc.)

**Usage:**
```go
// All header logging MUST use sanitization
logging.Debugf("Headers: %s", logging.SanitizeHeadersString(req.Header))
```

See `docs/security/secret-handling.md` for detailed documentation.

### Environment Variable Interpolation

API keys and other sensitive values use `${VAR_NAME}` syntax in configuration files. Values are resolved at load time from the process environment — secrets are never written to disk in plaintext.

```json
{
  "providers": {
    "openrouter": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}"
    }
  }
}
```

### Request Size Limits

The proxy enforces a default maximum request body size (50 MB) to protect against oversized payloads. Per-provider overrides are available via the `maxRequestBodyBytes` configuration option.

### Admin API Protection

Runtime profile management endpoints (switch, status) are secured with generated bearer tokens. See `docs/configuration.md` for token configuration.
