# Testing Guide

This document describes the testing strategy, organization, and best practices for cc-modelrouter.

---

## Running Tests

### Run All Tests

```bash
go test ./...
```

### Run Tests with Coverage

```bash
go test ./... -cover
```

### Run Tests with Coverage Report

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Run Tests with Verbose Output

```bash
go test ./... -v
```

### Run Specific Package Tests

```bash
go test ./internal/router/... -v
go test ./internal/proxy/... -v
```

### Run Specific Test

```bash
go test ./internal/router/... -v -run TestThinkLevelDetection
```

### Run Integration Tests

```bash
# Requires test configuration file at .cc-modelrouter/test.config.json
go test -tags=integration ./test/... -v
```

### Run Tests for Specific Test Files

```bash
# Run individual test files directly (bypasses package tests)
go test ./internal/config/loader_test.go ./internal/config/loader.go
go test ./internal/daemon/pidfile_test.go ./internal/daemon/pidfile.go
go test ./internal/daemon/instance_test.go ./internal/daemon/instance.go
```

---

## Test Organization

<!-- AUTO-GENERATED:START:test-organization -->
Tests follow Go's black-box/white-box testing patterns. See `.githooks/pre-commit` for enforcement.

| Test Type | Location | Package | Access |
|-----------|----------|---------|--------|
| **Black-box** | `<module>/test/` | `package <module>_test` | Exported members only |
| **White-box** | `<module>/` (alongside source) | `package <module>` | Private + exported members |
| **Cross-module** | `test/` (root) | Varies | Multiple modules |

```
cc-modelrouter/
├── internal/
│   ├── cli/
│   │   ├── adapters_test.go            # White-box CLI adapter tests (7)
│   │   ├── clean_test.go               # White-box clean command tests (7)
│   │   ├── code_profile_test.go        # White-box profile slash command tests (1)
│   │   ├── profile_integration_test.go # White-box profile integration tests (7)
│   │   ├── profile_test.go             # White-box profile command tests (43)
│   │   ├── root_test.go                # White-box CLI root tests (19)
│   │   └── status_test.go              # White-box status command tests (3)
│   ├── config/
│   │   ├── loader_test.go              # White-box config loading tests (2)
│   │   ├── types_test.go               # White-box config type tests (3)
│   │   └── test/
│   │       └── loglevel_test.go        # Black-box log level tests (3)
│   ├── configwizard/
│   │   ├── connectivity_test.go        # White-box connectivity tests (22)
│   │   ├── profile_test.go             # White-box profile wizard tests (23)
│   │   ├── shell_test.go              # White-box shell config tests
│   │   └── wizard_test.go              # White-box wizard model tests (75)
│   ├── daemon/
│   │   ├── instance_test.go            # White-box instance tests (18)
│   │   └── pidfile_test.go             # White-box PID file tests (13)
│   ├── interceptor/
│   │   ├── max_token_test.go           # White-box max tokens interceptor tests (12)
│   │   ├── reasoning_test.go           # White-box reasoning interceptor tests (15)
│   │   └── tool_enhance_test.go        # White-box tool enhance interceptor tests (16)
│   ├── logging/
│   │   ├── logging_test.go             # White-box logging tests (9)
│   │   └── sanitize_test.go            # White-box sanitization tests (4)
│   ├── monitor/
│   │   ├── buffer_test.go              # White-box ring buffer tests (7)
│   │   ├── model_test.go               # White-box TUI model tests (25)
│   │   ├── poller_test.go              # White-box stats poller tests (2)
│   │   ├── tailer_test.go              # White-box log tailer tests (3)
│   │   └── view_test.go                # White-box view helper tests (4)
│   ├── provider/
│   │   ├── client_test.go              # White-box client tests (5)
│   │   ├── http_test.go                # White-box HTTP client tests (23)
│   │   ├── streaming_timeout_test.go   # White-box streaming timeout tests (4)
│   │   └── test/
│   │       └── repro_test.go           # Black-box provider reproduction tests (2)
│   ├── proxy/
│   │   ├── beta_header_test.go         # White-box beta header tests (1)
│   │   ├── compactor_test.go           # White-box compactor tests (9)
│   │   ├── admin_handler_test.go       # White-box admin handler tests (15)
│   │   ├── deepcopy_test.go            # White-box deep copy tests (10)
│   │   ├── error1213_repro_test.go     # White-box error 1213 repro tests (5)
│   │   ├── files_handler_test.go       # White-box files API tests (5)
│   │   ├── handler_image_test.go       # White-box image handling tests (7)
│   │   ├── handler_test.go             # White-box handler tests (36)
│   │   ├── interceptor_test.go         # White-box interceptor tests (19)
│   │   ├── server_test.go              # White-box server tests (19)
│   │   ├── streaming_test.go           # White-box streaming tests (26)
│   │   └── streaming_timeout_test.go   # White-box streaming timeout tests (2)
│   ├── router/
│   │   ├── engine_test.go              # White-box route detection tests (15)
│   │   └── failover_test.go            # White-box failover tests (4)
│   ├── transformer/
│   │   ├── base_test.go                # White-box base transformer tests (7)
│   │   ├── registry_test.go            # White-box registry tests (4)
│   │   ├── test/
│   │   │   └── integration_test.go     # Black-box integration tests (7)
│   │   └── transformers/
│   │       ├── anthropic_normalization_test.go   # White-box normalization tests (3)
│   │       ├── anthropic_transformer_test.go     # White-box Anthropic tests (2)
│   │       ├── bigmodel_debug_test.go           # White-box BigModel debug tests (5)
│   │       ├── content_block_test.go            # White-box content block tests (1)
│   │       ├── gemini_test.go                   # White-box Gemini transformer tests (22)
│   │       ├── glm_truncation_test.go           # White-box GLM truncation tests (1)
│   │       ├── image_streaming_test.go          # White-box image streaming tests (7)
│   │       ├── integration_test.go              # White-box integration tests (3)
│   │       ├── normalization_test.go            # White-box normalization tests (20)
│   │       ├── openrouter_conversation_history_test.go  # White-box conversation history (2)
│   │       ├── openrouter_fix_test.go           # White-box OpenRouter fix tests (1)
│   │       ├── openrouter_transformer_test.go   # White-box OpenRouter tests (3)
│   │       └── repro_test.go                   # White-box reproduction tests (4)
│   └── usage/
│       ├── db_test.go                 # White-box database tests (5)
│       ├── formatter_test.go          # White-box formatter tests (3)
│       ├── period_test.go             # White-box period tests (4)
│       ├── stats_test.go              # White-box stats tests (3)
│       └── tracker_test.go            # White-box tracker tests (6)
├── pkg/
│   └── api/
│       └── anthropic/
│           ├── types_test.go          # White-box API type tests (19)
│           └── test/
│               ├── thinking_content_test.go     # Black-box thinking tests (13)
│               ├── types_document_test.go      # Black-box document tests (2)
│               ├── types_files_spec_test.go    # Black-box files spec tests (1)
│               └── types_image_test.go         # Black-box image tests (6)
└── test/                              # Cross-module integration tests
    ├── aliyun_test.go                 # Aliyun provider tests (4)
    ├── integration_sse_test.go        # SSE integration tests (1)
    ├── integration_test.go            # Basic integration tests (1)
    ├── openrouter_test.go             # OpenRouter tests (2)
    ├── security/
    │   └── secret_logging_test.go     # Security tests (6)
    └── integration/
        ├── cli/code_command_test.go         # CLI code command tests (15)
        ├── error/
        │   ├── edge_case_test.go            # Edge case error tests (11)
        │   ├── error_recovery_test.go       # Error recovery tests (7)
        │   └── malformed_response_test.go   # Malformed response tests (4)
        ├── files/pdf_test.go                # PDF file tests (9)
        ├── images/provider_image_test.go    # Provider image tests (7)
        ├── load/
        │   ├── concurrent_test.go           # Concurrent load tests (8)
        │   └── stress_test.go               # Stress tests (8)
        ├── network/
        │   ├── connection_failure_test.go   # Connection failure tests (8)
        │   ├── partial_response_test.go     # Partial response tests (8)
        │   ├── rate_limit_test.go           # Rate limit tests (7)
        │   ├── retry_logic_test.go          # Retry logic tests (6)
        │   └── timeout_test.go              # Timeout tests (6)
        ├── provider_quirks/
        │   ├── gemini_quirks_test.go        # Gemini quirks tests (7)
        │   ├── openrouter_quirks_test.go    # OpenRouter quirks tests (7)
        │   └── qwen_quirks_test.go          # Qwen quirks tests (8)
        ├── real_api/
        │   ├── aliyun_real_test.go          # Aliyun real API tests (5)
        │   ├── bigmodel_real_test.go        # BigModel real API tests (9)
        │   ├── failover_real_test.go        # Failover real API tests (4)
        │   ├── glm_fix_test.go              # GLM fix tests (4)
        │   └── openrouter_real_test.go      # OpenRouter real API tests (9)
        └── usage_tracking_test.go           # Usage tracking tests (5)
```
<!-- AUTO-GENERATED:end:test-organization -->

---

## Test Categories

### Unit Tests

Unit tests verify individual functions and methods in isolation.

**Location:** `*_test.go` files alongside source code

**Example - Testing thinking level detection:**
```go
func TestGetThinkLevel(t *testing.T) {
    handler := NewHandler(50 * 1024 * 1024)

    tests := []struct {
        name     string
        thinking *anthropic.ThinkingConfig
        expected router.ThinkLevel
    }{
        {"nil thinking", nil, router.ThinkNone},
        {"thinking with budget 32000", &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 32000}, router.ThinkHighest},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := &anthropic.Request{Thinking: tt.thinking}
            result := handler.getThinkLevel(req)
            if result != tt.expected {
                t.Errorf("getThinkLevel() = %v, expected %v", result, tt.expected)
            }
        })
    }
}
```

### Integration Tests

Integration tests verify multiple components working together.

**Location:** `test/integration_test.go`

**Prerequisites:**
- Test configuration file at `.cc-modelrouter/test.config.json`
- Valid API credentials for configured providers
- Network access to provider endpoints

**Running:**
```bash
go test -tags=integration ./test/... -v
```

#### Integration Test Scenarios

| Test | Description |
|------|-------------|
| `TestIntegrationBasicRequest` | End-to-end request through proxy with usage tracking verification |

The integration test validates:
1. Configuration loading
2. Transformer initialization
3. Provider client creation
4. Router engine setup
5. Proxy handler request processing
6. Usage tracking after successful response

---

## Current Test Coverage

| Package | Test Files | Test Count | Status |
|---------|------------|------------|--------|
| `internal/cli` | adapters_test.go, clean_test.go, code_profile_test.go, profile_test.go, profile_integration_test.go, root_test.go, status_test.go | 80 | ✓ |
| `internal/config` | loader_test.go, types_test.go, test/loglevel_test.go | 8 | ✓ |
| `internal/configwizard` | connectivity_test.go, profile_test.go, shell_test.go, wizard_test.go | 111 | ✓ |
| `internal/daemon` | instance_test.go, pidfile_test.go | 31 | ✓ |
| `internal/interceptor` | max_token_test.go, reasoning_test.go, tool_enhance_test.go | 43 | ✓ |
| `internal/logging` | logging_test.go, sanitize_test.go | 13 | ✓ |
| `internal/monitor` | buffer_test.go, model_test.go, poller_test.go, tailer_test.go, view_test.go | 43 | ✓ |
| `internal/provider` | client_test.go, http_test.go, streaming_timeout_test.go, test/repro_test.go | 34 | ✓ |
| `internal/proxy` | admin_handler_test.go, deepcopy_test.go, error1213_repro_test.go, files_handler_test.go, handler_image_test.go, handler_test.go, interceptor_test.go, server_test.go, streaming_test.go, streaming_timeout_test.go, beta_header_test.go, compactor_test.go | 156 | ✓ |
| `internal/router` | engine_test.go, failover_test.go | 15 | ✓ |
| `internal/transformer` | base_test.go, registry_test.go, test/integration_test.go, transformers/*.go (incl. gemini_test.go) | 87 | ✓ |
| `internal/usage` | db_test.go, tracker_test.go, period_test.go, stats_test.go, formatter_test.go | 21 | ✓ |
| `pkg/api/anthropic` | types_test.go, test/*.go (4 files) | 41 | ✓ |
| `test/` (integration) | files, images, security, integration/ | 22 | ✓ |

**Overall:** **726 tests** across 23 packages

---

## Coverage Goals

| Module | Current Coverage | Target | Status |
|--------|------------------|--------|--------|
| `daemon` | ~88% | 90%+ | ~ |
| `interceptor` | ~92% | 90%+ | ✓ |
| `transformer` (core) | ~96% | 90%+ | ✓ |
| `usage` | ~89% | 90%+ | ~ |
| `logging` | ~80% | 90% | ~ |
| `router` | ~87% | 90% | ~ |
| `proxy` | ~79% | 90% | ~ |
| `provider` | ~71% | 90% | ~ |
| `anthropic` | ~69% | 90% | ~ |
| `transformer/transformers` | ~45% | 90% | ~ |
| `monitor` | ~36% | 60%+ | ~ |
| `config` | ~60% | 90% | ~ |
| `configwizard` | ~25% | 60%+ | ~ |
| `cli` | ~29% | 50%+ | ~ |

**Overall:** ~46% average coverage across 23 packages

---

## Key Test Suites

### Router Engine Tests (`internal/router/engine_test.go`)

Tests for route detection logic:

| Test | Description |
|------|-------------|
| `TestRouteDetection` | Basic route detection (default, background, longContext) |
| `TestThinkLevelDetection` | Thinking level detection with full config |
| `TestThinkLevelFallback` | Fallback behavior with partial config |
| `TestThinkLevelPartialConfig` | Middle tier fallback (ultrathink → thinkMore → think) |
| `TestRoutePriority` | Route priority ordering |
| `TestThinkLevelNoRouteConfigured` | Fallback to default when no think routes |
| `TestGetTargets` | Target retrieval and default fallback |
| `TestProfileBasedRouting` | Profile-aware route selection |
| `TestProfileSwitching` | Hot-reload profile switching |
| `TestProfileBasedRouting_ThreadSafe` | Concurrent profile access with mutex |
| `TestProfileFallbackToLegacy` | Legacy route fallback when no profiles |
| `TestFailover*` | Failover iteration and exhaustion tests |

### Proxy Handler Tests (`internal/proxy/handler_test.go`)

Tests for HTTP handling and request analysis:

| Test | Description |
|------|-------------|
| `NewHandler*` | Handler creation with different request sizes |
| `ServeHTTP_*` | HTTP method/path validation and error handling |
| `handleMessages_*` | Message processing and failover |
| `tryTarget_*` | Provider/transformer/client error handling |
| `estimateTokens_*` | Token estimation from message content |
| `hasWebSearch_*` | Web search detection via tool names |
| `hasImages_*` | Image detection in message content |
| `isBackground_*` | Background agent detection via model name |
| `getThinkLevel_*` | Thinking level detection with edge cases |
| `Handler_Setters` | Dependency injection for router, registry, clients |

### Proxy Admin Handler Tests (`internal/proxy/admin_handler_test.go`)

Tests for admin API profile management:

| Test | Description |
|------|-------------|
| `TestAdminHandler_ListProfiles` | List profiles with active marker |
| `TestAdminHandler_GetActiveProfile` | Get current active profile |
| `TestAdminHandler_SwitchProfile` | Switch to a different profile |
| `TestAdminHandler_SwitchProfile_InvalidProfile` | Error on non-existent profile |
| `TestAdminHandler_SwitchProfile_EmptyProfile` | Error on empty profile name |
| `TestAdminHandler_SwitchProfile_InvalidBody` | Error on malformed request body |
| `TestAdminHandler_SwitchProfile_ReturnsProfileDetails` | Response includes profile name and routes |
| `TestAdminHandler_Unauthorized` | Auth rejection for missing/wrong token |
| `TestAdminHandler_TokenInQueryParam` | Token accepted via query parameter |
| `TestAdminHandler_LocalhostOnly` | Rejection for non-localhost requests |
| `TestAdminHandler_UnknownEndpoint` | 404 for unrecognized admin paths |
| `TestAdminHandler_ListProfiles_Sorted` | Profiles returned in alphabetical order |
| `TestAdminHandler_ListProfiles_LegacyRoutes` | Legacy routes shown when no profiles |

### CLI Profile Command Tests (`internal/cli/profile_test.go`, `profile_integration_test.go`)

Tests for profile CLI commands:

| Test | Description |
|------|-------------|
| `TestNewProfileCommand*` | Profile command structure and subcommands |
| `TestNewProfileListCommand` | List command flags and defaults |
| `TestNewProfileSwitchCommand` | Switch command args validation |
| `TestNewProfileStatusCommand` | Status command flags |
| `TestProfileSwitchWorkflow_Integration` | Full workflow: config → engine → admin API → switch |
| `TestProfileSwitchWithRouterEngine_Integration` | Engine profile switching with route verification |
| `TestProfileListCommand_OutputFormatting` | Output format with asterisk marker |
| `TestPrintProfileList_FromConfig` | Direct function call with config object |

### Proxy Server Tests (`internal/proxy/server_test.go`)

Tests for HTTP server lifecycle:

| Test | Description |
|------|-------------|
| `Defaults` | Default server configuration values |
| `NewServer*` | Server creation with various configurations |
| `Server_Setters` | Dependency injection methods |
| `Server_TimeoutConfiguration` | Read/write timeout verification |
| `Server_HandlerCreated` | Handler initialization |
| `Server_HandlerMaxRequestSize` | Handler size configuration |
| `Server_ShutdownWithUsageTracker` | Usage tracker shutdown on stop |
| `Server_ConcurrentIsRunning` | Concurrent IsRunning() calls |
| `Server_PortZero` | Port zero handling |
| `Server_Start*` / `Server_Stop*` | Start/stop behavior |

### Proxy Streaming Tests (`internal/proxy/streaming_test.go`)

Tests for SSE streaming:

| Test | Description |
|------|-------------|
| `NewSSEWriter*` | Writer creation with/without Flusher |
| `WriteEvent*` | Event writing with various data |
| `Flush*` | Flushing behavior |
| `ParseSSEEvent*` | Event parsing for various formats |
| `SSEScanner_Scan*` | Scanner event scanning |
| `SSEScanner_*Method*` | Scanner method returns |
| `SSEScanner_*` | Scanner data/event handling |

### Daemon Tests (`internal/daemon/instance_test.go`, `pidfile_test.go`)

Tests for instance management and PID file operations:

| Test | Description |
|------|-------------|
| `GenerateInstanceID*` | ID generation and uniqueness |
| `InstanceMetadata*` | Metadata JSON marshaling/unmarshaling |
| `InstancesDir_Success` | Instances directory path retrieval |
| `IsRunning_*` | Process running status checks |
| `SaveAndLoadInstance*` | Instance persistence |
| `DeleteInstance*` | Instance cleanup |
| `LoadInstance*` | Instance loading errors |
| `ListInstances*` | Instance listing with filters |
| `WritePIDFile*` | PID file creation and permissions |
| `ReadPIDFile*` | PID file reading and parsing |

### Provider HTTP Client Tests (`internal/provider/http_test.go`)

Tests for HTTP client with retry logic:

| Test | Description |
|------|-------------|
| `NewClient_*` | Client creation with various configurations |
| `Do_Success` | Successful request without retries |
| `Do_RetryOn*` | Retry behavior for 5xx errors |
| `Do_NoRetryOn4xx` | No retry on 4xx errors |
| `Do_MaxRetriesExceeded` | Error after max retries |
| `Do_ContextCancellation` | Context cancellation handling |
| `Do_NetworkError` | Network error handling |
| `DoWithContext_*` | Context forwarding |
| `Do_Timeout` | Request timeout handling |
| `Do_ReadBody` | Response body reading |
| `Do_ErrorFormat` | Error message format |

### CLI Adapter Tests (`internal/cli/adapters_test.go`)

Tests for CLI adapter wrappers:

| Test | Description |
|------|-------------|
| `NewRouterAdapter*` | Router adapter creation and delegation |
| `NewTransformerAdapter*` | Transformer adapter creation and delegation |
| `NewRegistryAdapter*` | Registry adapter creation and delegation |
| `TransformerAdapter_*` | All interface method delegations |
| `RegistryAdapter_*` | Get success and not-found scenarios |

### CLI Root Command Tests (`internal/cli/root_test.go`)

Tests for CLI command structure:

| Test | Description |
|------|-------------|
| `NewRootCommand*` | Root command creation and properties |
| `NewRootCommand_*Command` | Subcommand verification |
| `Version` | Version string verification (accepts `X.Y.Z`, `dev-<hex>`, or `<tag>-<hex>`) |
| `Execute_*` | Execute function behavior |
| `NewRootCommand_PersistentFlags` | Flag configuration |

### Transformer Tests (`internal/transformer/*_test.go`)

Tests for each provider transformer:

| Test | Description |
|------|-------------|
| `TestAnthropicTransform*` | Anthropic format transformation |
| `TestGeminiTransform*` | Gemini API format transformation |
| `TestOpenAITransform*` | OpenAI-compatible format transformation |
| `TestGLMTransform*` | Zhipu GLM format transformation |
| `TestOpenRouterTransform*` | OpenRouter API format transformation |
| `TestRegistry*` | Transformer registry management |

### Usage Tracking Tests (`internal/usage/*_test.go`)

Tests for usage tracking functionality:

| Test | Description |
|------|-------------|
| `TestDB*` | SQLite database initialization and operations |
| `TestTracker*` | Buffer flushing, concurrent access, shutdown |
| `TestPeriod*` | Period parsing for time ranges |
| `TestStats*` | Statistics aggregation |
| `TestFormatter*` | Token number formatting |

---

## Testing Route Detection

### Thinking Levels

The thinking level detection is tested with various budget_tokens values:

```go
// Budget thresholds
ThinkLevelBasic   = 4000   // "think"
ThinkLevelMiddle  = 10000  // "think more", "think hard"
ThinkLevelHighest = 32000  // "ultrathink"
```

**Test cases:**
- `budget_tokens = 0` → `ThinkNone`
- `budget_tokens = 1000` → `ThinkBasic`
- `budget_tokens = 4000` → `ThinkBasic`
- `budget_tokens = 10000` → `ThinkMiddle`
- `budget_tokens = 20000` → `ThinkMiddle`
- `budget_tokens = 32000` → `ThinkHighest`
- `budget_tokens = 50000` → `ThinkHighest`

### Route Priority

Routes are checked in this priority order:

1. `background` - Model name contains both `claude` and `haiku` (Claude Code uses Haiku for background agents)
2. `subagent` - HTTP headers `X-Claude-Code-Agent-Id` / `X-Claude-Code-Parent-Agent-Id`; fallback to subagent keywords in tools/last user message
3. `review` - Last user message contains "/review", "code review", "review this", "review the", or starts with "review "
4. `ultrathink` - Highest thinking level (budget_tokens >= 32000)
5. `thinkMore` - Middle thinking level (budget_tokens >= 10000)
6. `think` - Basic thinking level (budget_tokens >= 4000)
7. `image` - Request contains image content
8. `webSearch` - Tool names contain "web" or "search"
9. `longContext` - Token count > 60,000
10. `default` - Fallback for all other requests

### Fallback Behavior

Thinking routes support flexible configuration:

```
ultrathink → thinkMore → think → default
thinkMore → think → default
think → default
```

---

## Writing New Tests

### Table-Driven Tests

Use table-driven tests for testing multiple scenarios:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case 1", "input1", "output1"},
        {"case 2", "input2", "output2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFunction(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Testing HTTP Handlers

For HTTP handler tests, use `httptest`:

```go
import "net/http/httptest"

func TestHandler(t *testing.T) {
    req := httptest.NewRequest("POST", "/v1/messages", body)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("expected status 200, got %d", rec.Code)
    }
}
```

### Mocking Interfaces

Use interfaces for mocking external dependencies:

```go
type MockTransformer struct {
    transformErr error
}

func (m *MockTransformer) TransformRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
    if m.transformErr != nil {
        return nil, m.transformErr
    }
    return httptest.NewRequest("POST", baseURL, nil), nil
}
```

### Using Temporary Directories

For file I/O tests, use `t.TempDir()` for automatic cleanup:

```go
func TestFileOperations(t *testing.T) {
    tmpDir := t.TempDir()
    filePath := filepath.Join(tmpDir, "test.txt")

    err := os.WriteFile(filePath, []byte("content"), 0600)
    if err != nil {
        t.Fatalf("failed to write file: %v", err)
    }
    // File is automatically cleaned up after test
}
```

---

## Continuous Integration

Tests are run automatically on:
- Pull requests
- Pushes to main branch
- Release tags

---

## Test Best Practices

1. **Test behavior, not implementation** - Focus on what the code does, not how
2. **Use descriptive test names** - Names should describe the scenario being tested
3. **Test edge cases** - Include boundary values, empty inputs, and error conditions
4. **Keep tests independent** - Each test should run independently of others
5. **Avoid test interdependence** - Tests should not rely on execution order
6. **Use `t.Parallel()` for parallel tests** - Speed up test execution where safe
7. **Clean up resources** - Use `t.Cleanup()` for resource cleanup
8. **Use `t.TempDir()` for temporary files** - Automatic cleanup after test
9. **Use table-driven tests** - For multiple similar test cases
10. **Mock external dependencies** - Use interfaces for mocking HTTP clients, databases, etc.

---

## Test Documentation

Test plans are tracked alongside implementation plans in `plans/`.