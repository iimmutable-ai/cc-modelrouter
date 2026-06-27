# Usage Tracking Tests

This document describes the integration tests for cc-modelrouter's usage tracking functionality.

## Overview

The usage tracking system:
- Stores token usage in SQLite at `~/.cc-modelrouter/usage.db`
- Tracks actual provider-reported tokens (input + output)
- Extracts usage from `message_delta` events in streaming responses
- Uses buffered writing with periodic flush

## Test File

`test/integration/usage_tracking_test.go`

## Running the Tests

### Using the test script:
```bash
./test/test-usage-tracking.sh
```

### Directly:
```bash
go test -tags=integration -v -run TestUsageTracking ./test/integration/...
```

## Test Cases

### 1. TestUsageTrackingNonStreaming
Tests usage tracking with non-streaming requests.

**What it verifies:**
- Tokens are tracked from provider response
- Usage record is persisted to database
- Route and model are correctly captured
- Input and output tokens are summed

**Expected behavior:**
- Makes a simple request to the LLM provider
- Receives response with usage data
- Verifies database contains matching usage record

### 2. TestUsageTrackingStreaming
Tests usage tracking with streaming requests.

**What it verifies:**
- Tokens are extracted from SSE `message_delta` events
- Accumulated output tokens are tracked
- Input tokens are captured when sent by provider (e.g., GLM)
- Falls back to estimation if provider doesn't send usage

**Expected behavior:**
- Makes streaming request
- Processes SSE events
- Extracts token usage from message_delta events
- Verifies database record

### 3. TestUsageTrackingFallback
Tests usage tracking with provider fallback scenarios.

**What it verifies:**
- Fallback count is correctly recorded
- Usage is tracked for the successful provider
- Route detection works correctly

**Expected behavior:**
- Makes request that may trigger fallback
- Records which provider succeeded
- Tracks fallback attempts

### 4. TestUsageTrackingConcurrent
Tests usage tracking with concurrent requests.

**What it verifies:**
- Thread-safe buffer operations
- All concurrent requests are tracked
- No race conditions in database writes

**Expected behavior:**
- Spawns 5 concurrent requests
- All requests complete successfully
- All 5 usage records are in database
- Total tokens match expected

### 5. TestUsageTrackingBufferedFlush
Tests the buffer flush mechanism.

**What it verifies:**
- Automatic flush when buffer is full
- Final flush on shutdown
- All records are persisted

**Expected behavior:**
- Creates tracker with buffer size of 3
- Adds 4 records (triggers automatic flush at 3)
- Verifies at least 3 records in database
- Calls shutdown
- Verifies all 4 records are persisted

## Test Output Example

```
=== RUN   TestUsageTrackingNonStreaming
=== RUN   TestUsageTrackingNonStreaming/Simple_Request
    usage_tracking_test.go:136: Response usage: input=10, output=3
    usage_tracking_test.go:165: ✓ Non-streaming usage tracked: route=default, model=glm-4.7, tokens=13
--- PASS: TestUsageTrackingNonStreaming (1.19s)
    --- PASS: TestUsageTrackingNonStreaming/Simple_Request (1.18s)
=== RUN   TestUsageTrackingStreaming
=== RUN   TestUsageTrackingStreaming/Streaming_Request
    usage_tracking_test.go:254: Streaming response length: 2417 bytes
    usage_tracking_test.go:282: ✓ Streaming usage tracked: route=default, model=glm-4.7, tokens=26
--- PASS: TestUsageTrackingStreaming (0.87s)
=== RUN   TestUsageTrackingFallback
=== RUN   TestUsageTrackingFallback/Request_With_Fallback
    usage_tracking_test.go:374: ✓ Fallback usage tracked: route=default, model=glm-4.7, tokens=11, fallbacks=0
--- PASS: TestUsageTrackingFallback (0.75s)
=== RUN   TestUsageTrackingConcurrent
=== RUN   TestUsageTrackingConcurrent/Concurrent_Requests
    usage_tracking_test.go:491: Record: route=default, model=glm-4.7, tokens=11
    usage_tracking_test.go:491: Record: route=default, model=glm-4.7, tokens=11
    usage_tracking_test.go:491: Record: route=default, model=glm-4.7, tokens=11
    usage_tracking_test.go:491: Record: route=default, model=glm-4.7, tokens=11
    usage_tracking_test.go:491: Record: route=default, model=glm-4.7, tokens=11
    usage_tracking_test.go:495: ✓ Concurrent usage tracked: 5 records, 55 total tokens
--- PASS: TestUsageTrackingConcurrent (1.00s)
=== RUN   TestUsageTrackingBufferedFlush
    usage_tracking_test.go:553: ✓ Buffered flush verified: 4 records written
--- PASS: TestUsageTrackingBufferedFlush (0.11s)
PASS
ok  	github.com/iimmutable-ai/cc-modelrouter/test/integration	4.281s
```

## Configuration

The tests require a test configuration file at `.cc-modelrouter/test.config.json` with:

- Provider API keys (for testing against real providers)
- Model configurations
- Route definitions

If the config is missing, tests will be skipped with a message.

## Key Implementation Details

### Token Extraction in Streaming

From `internal/proxy/handler.go:tryStreamingTarget`:

```go
// Extract usage data from message_delta events
for _, te := range transformedEvents {
    if te.EventType == "message_delta" {
        var eventData map[string]interface{}
        if json.Unmarshal(te.Data, &eventData) == nil {
            if usage, ok := eventData["usage"].(map[string]interface{}); ok {
                // Extract output tokens
                if outputTokens, ok := usage["output_tokens"].(float64); ok {
                    totalOutputTokens += int(outputTokens)
                }
                // Extract input tokens if provider sends it (e.g., GLM)
                if inputTokens, ok := usage["input_tokens"].(float64); ok {
                    totalInputTokens = int(inputTokens)
                }
            }
        }
    }
}
```

### Database Schema

From `internal/usage/db.go`:

```sql
CREATE TABLE IF NOT EXISTS usage_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT NOT NULL,
    route TEXT NOT NULL,
    model TEXT NOT NULL,
    tokens INTEGER NOT NULL,
    fallbacks INTEGER NOT NULL DEFAULT 0,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Troubleshooting

### Tests are skipped
- Check that `.cc-modelrouter/test.config.json` exists
- Verify API keys are configured

### Database errors
- Ensure write permissions for `~/.cc-modelrouter/`
- Check that SQLite is available

### Token count mismatches
- Some providers estimate tokens differently
- Streaming may use actual counts, non-streaming may use estimates
- GLM sends exact input_tokens in message_delta events

## Future Enhancements

Potential additional tests:
- Test with different providers (OpenAI, Anthropic, etc.)
- Test with thinking-enabled requests
- Test with image content
- Test with very large responses (buffer overflow)
- Test database query performance with many records
- Test usage statistics calculation
