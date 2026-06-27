# CLAUDE.md

AI-specific guidance for Claude Code when working with cc-modelrouter.

---

## 🚀 Quick Command Reference

### Essential Build & Test Commands
```bash
# Build debug binary (branch-aware: "dev" tag on dev-local, git-tag elsewhere)
make build

# Build release binary (stripped symbols, same branch-aware version)
make release

# Quick single-platform debug build (no Linux cross-compile)
go build -o bin/debug/ccrouter ./cmd/ccrouter

# NOTE: Plain `go build` (no ldflags) still reports a valid version via runtime
# git detection (e.g., "dev-2661291a" on dev-local). Prefer `make build` for the
# canonical cross-compiled build.

# Run all tests
go test ./...

# Test with coverage
go test ./... -cover

# Generate coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run specific test
go test ./internal/router -run TestThinkLevelDetection

# Run security tests
go test -v ./test/security
```

### Essential Runtime Commands
```bash
# Start router + Claude Code (most common)
./bin/ccrouter code

# Start standalone
./bin/ccrouter start

# Start with debug logging
./bin/ccrouter start --log-level=debug --log-destination=file

# Check status
./bin/ccrouter status

# Stop instance
./bin/ccrouter stop <instance-id>

# Stop all
./bin/ccrouter stop

# Manage route profiles
./bin/ccrouter profile list
./bin/ccrouter profile switch <profile-name>
./bin/ccrouter profile status

# Interactive configuration wizard
./bin/ccrouter config

# Live usage monitor
./bin/ccrouter monitor
```

### Critical File Paths
```
~/.cc-modelrouter/config.json          # Global configuration
<project>/.cc-modelrouter/config.json  # Project config (overrides global ENTIRELY)
~/.cc-modelrouter/usage.db             # SQLite usage tracking
~/.cc-modelrouter/logs/inst_*.log      # Instance logs
~/.cc-modelrouter/instances/*.json     # Instance metadata
```

---

## 📐 Architecture Quick Reference

For full architecture diagrams and detailed explanations, see [README.md](README.md#architecture).

**Core Request Flow:**
```
Claude Code → HTTP Proxy → Router Engine → Transformer → Provider API
                  ↓              ↓              ↓
            (validate)      (detect route   (convert
                            & select        request)
                            provider)

Provider API → Transformer → HTTP Proxy → Claude Code
                  ↓              ↓
           (convert        (stream
            response)      response)
```

**Key Components:**
- `internal/proxy/` - HTTP proxy, Anthropic Messages API endpoint
- `internal/router/` - Route detection, provider selection, failover
- `internal/transformer/` - Direct format conversion (Anthropic ↔ Provider)
- `internal/usage/` - SQLite usage tracking with buffered writes
- `internal/configwizard/` - Interactive TUI configuration wizard (Bubble Tea)
- `internal/monitor/` - Live usage monitor terminal UI
- `internal/interceptor/` - Request/response/streaming interceptors
- `internal/daemon/` - Instance management (PID files, metadata)

---

## ⚠️ Critical Implementation Rules

### 1. Usage Tracking in Streaming (CRITICAL)

Some providers (GLM) send both `input_tokens` and `output_tokens` in `message_delta` events during streaming:

```go
// In handler.go tryStreamingTarget - extract from message_delta:
var totalInputTokens int
var totalOutputTokens int

if te.EventType == "message_delta" {
    if usage, ok := eventData["usage"].(map[string]interface{}); ok {
        if outputTokens, ok := usage["output_tokens"].(float64); ok {
            totalOutputTokens += int(outputTokens)
        }
        if inputTokens, ok := usage["input_tokens"].(float64); ok {
            totalInputTokens = int(inputTokens)  // Use actual, not estimate
        }
    }
}
```

**Fallback:** If provider doesn't send `input_tokens` in streaming, fall back to `estimateTokens(req)` (÷4 character count).

---

### 2. Secret Handling in Logs (CRITICAL - SECURITY)

**API keys, tokens, and secrets MUST NEVER appear in log files.**

**Sensitive headers automatically redacted:**
- `Authorization` (Bearer tokens)
- `X-Api-Key` (Anthropic API keys)
- `X-Auth-Token`
- `Cookie` / `Set-Cookie`
- `Proxy-Authorization`
- `X-BigModel-Api-Key`
- `X-OpenRouter-Api-Key`

**Always use sanitization library:**

```go
import "github.com/iimmutable-ai/cc-modelrouter/internal/logging"

// ✅ CORRECT - Headers are sanitized:
logging.Debugf("[PROXY REQUEST] Headers: %s", logging.SanitizeHeadersString(req.Header))

// ❌ WRONG - Leaks API keys:
logging.Debugf("[PROXY REQUEST] Headers: %v", req.Header)
```

**Redaction format:**
```
X-Api-Key:[sk-ant-**************** [REDACTED]]
Authorization:[Bearer**************** [REDACTED]]
```

**Verify before commit:**
```bash
go test -v ./test/security
```

---

### 3. Files API Status (IMPORTANT - NOT IMPLEMENTED)

**Claude Code does NOT use Anthropic's Files API.**

**What IS implemented:**
- API endpoints `/v1/files` (for API completeness)
- Type support: `DocumentSource` with `file_id` references
- Document block types in transformers

**What is NOT implemented:**
- File storage (mock responses only)
- File resolution (`file_id` not resolved)
- File content retrieval

**Behavior when request contains document block with `file_id`:**

For Anthropic provider:
- Passes through unchanged
- **BUT won't work** - we don't proxy to real Anthropic Files API

For other providers (OpenAI, Gemini, GLM):
- Logs warning
- Uses placeholder text: `[Document: Report.pdf - file_id: file-abc123]`

**Why this exists:**
- API specification compliance
- Future-proofing if Files API support needed
- Type safety for request parsing

**Since Claude Code doesn't use Files API, implementing storage/resolution would be YAGNI.**

**If needed:** See `plans/2026-03-06-file-resolution-layer-implementation.md`

---

### 4. Test File Organization (ENFORCED BY PRE-COMMIT HOOK)

Tests must follow Go's black-box/white-box testing patterns:

| Test Type | Location | Package | Access |
|-----------|----------|---------|--------|
| **Black-box** | `<module>/test/` | `package <module>_test` | Exported members only |
| **White-box** | `<module>/` (alongside source) | `package <module>` | Private + exported members |
| **Cross-module** | `test/` (root) | Varies | Multiple modules |

**Rules:**
- `<module>/test/` files **must** use `package <module>_test` and **must only reference exported symbols**
- If a test references unexported symbols, it **must** be alongside source with `package <module>`
- **Never** move a white-box test to a `test/` subdirectory — package isolation prevents access to unexported symbols and it will break compilation

**Examples:**

| Location | Status | Reason |
|----------|--------|--------|
| `internal/proxy/test/handler_test.go` | ✅ Correct | Black-box test |
| `internal/proxy/handler_test.go` | ✅ Allowed | White-box (needs private access) |
| `test/integration/provider_test.go` | ✅ Correct | Cross-module test |
| `internal/proxy/utils_test.go` (no `utils.go`) | ❌ Wrong | Should be in `test/` subfolder |
| `<module>/test/foo_test.go` with `package <module>` | ❌ Wrong | Wrong package — use `_test` suffix |
| `<module>/test/foo_test.go` referencing unexported symbol | ❌ Wrong | Must be alongside source |

**Pre-commit hook validates both placement and compilation.** See `.githooks/pre-commit`.

---

## 🔧 Implementation Patterns

### Adding a New Transformer

1. **Create transformer** in `internal/transformer/transformers/<name>.go`
2. **Implement `Transformer` interface:**
   - `Name()` - Transformer identifier
   - `Endpoint()` - API endpoint path
   - `PrepareRequest()` - Convert Anthropic → Provider HTTP request
   - `ParseResponse()` - Convert Provider HTTP response → Anthropic
   - `SupportsStreaming()` - Boolean
   - `TransformStreamEvent()` - Convert Provider SSE → Anthropic SSE
3. **Register** in `internal/cli/start.go` or `internal/cli/code.go`
4. **Handle streaming:** Generate synthetic `message_delta` events with `output_tokens` if provider doesn't send them

**SSE events to generate for streaming:**
- `message_start`
- `content_block_delta`
- `message_delta` (must include `usage.output_tokens`)
- `message_stop`

---

### Failover Strategy

Router implements **sequential failover**:

1. Try each `provider:model` in route config order
2. After exhausting list, loop back to beginning
3. Maximum attempts: `2 × number of providers`
4. Each attempt = 1 request (no per-provider retries by default)

**Example:**
```json
"default": "openrouter:claude-sonnet-4;bigmodel:glm-4.7"
```

Try order: `openrouter` → `bigmodel` → `openrouter` → `bigmodel` (max 4 attempts)

---

### Request Interceptors

Interceptors provide cross-cutting concerns at three points:

- **Request interceptors:** Before routing (validation, logging)
- **Response interceptors:** After provider response (metrics, validation)
- **Streaming interceptors:** Per SSE event (modification, filtering)

Add via handler methods:
- `AddRequestInterceptor(fn)`
- `AddResponseInterceptor(fn)`
- `AddStreamingInterceptor(fn)`

---

## 🎯 Route Detection Logic

Router automatically selects routes based on request characteristics (priority order matters — checked top to bottom):

| Priority | Route | Trigger | Detection Method |
|----------|-------|---------|-----------------|
| 1 | `background` | Background agent request | Model name contains both `claude` and `haiku` (case-insensitive) |
| 2 | `subagent` | Subagent task | HTTP headers `X-Claude-Code-Agent-Id` / `X-Claude-Code-Parent-Agent-Id`; fallback: tool name contains "subagent", or last user message contains "subagent"/"delegate to agent" |
| 3 | `review` | Review task | Last user message contains "/review", "code review", "review this", "review the", or starts with "review " |
| 4 | `ultrathink` | Maximum thinking | `budget_tokens >= 32,000` |
| 5 | `thinkMore` | Enhanced thinking | `budget_tokens >= 10,000` |
| 6 | `think` | Basic thinking | `budget_tokens >= 4,000` |
| 7 | `image` | Image content | Request contains image blocks |
| 8 | `webSearch` | Web search enabled | Tool names contain "web"/"search" |
| 9 | `longContext` | Large context | Token count > 60,000 |
| 10 | `default` | Fallback | All other requests |

**Thinking level fallback:** If `ultrathink` not configured, falls back to `thinkMore`, then `think`.

---

## ⚙️ Configuration

### File Locations
- **Global:** `~/.cc-modelrouter/config.json`
- **Project:** `<project>/.cc-modelrouter/config.json`

**Important:** Project config **completely overrides** global config (not merged).

### Environment Variables
Use `${VAR_NAME}` syntax for API keys:
```json
{
  "providers": {
    "openrouter": {
      "apiKey": "${CCROUTER_OPENROUTER_API_KEY}"
    }
  }
}
```

Required environment variables:
- `CCROUTER_OPENROUTER_API_KEY`
- `CCROUTER_GEMINI_API_KEY`
- `CCROUTER_BIGMODEL_API_KEY`
- etc. (depends on providers used)

### Route Format
Semicolon-separated `provider:model` pairs for failover:
```json
{
  "router": {
    "routes": {
      "default": "openrouter:anthropic/claude-sonnet-4;bigmodel:glm-4.7",
      "think": "openrouter:anthropic/claude-opus-4"
    }
  }
}
```

---

## 🧪 Testing Patterns

### Table-Driven Tests
For multiple scenarios:
```go
tests := []struct{
    name string
    input string
    want string
}{
    {"case1", "input1", "output1"},
    {"case2", "input2", "output2"},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
    })
}
```

### Mock HTTP Clients
Use `httptest.NewServer` for provider testing:
```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // mock response
}))
defer server.Close()
```

### Coverage
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## 🚨 Error Handling

| Error Type | Behavior |
|------------|----------|
| Provider errors | Return to client with proper HTTP status codes |
| Transform errors | Log and skip invalid SSE events (don't fail stream) |
| Timeout errors | Configurable per provider in config |
| All errors | Log with context (provider, model, route) |

---

## 📝 Log File Locations

- **Instance logs:** `~/.cc-modelrouter/logs/inst_YYYYMMDD_HHMMSS.log`
- **Per-instance:** Each `ccrouter code` instance gets own log file
- **Levels:** debug, info, warn, error (configurable via `--log-level`)

---

## 📚 For More Information

See [README.md](README.md) for:
- Installation and quick start guide
- Full architecture diagrams
- Provider compatibility tables
- Unified intermediate format specification
- Complete project structure
- Transformer interface details