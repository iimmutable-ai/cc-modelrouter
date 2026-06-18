# Troubleshooting

Common issues and their solutions when using ccrouter.

## Port Conflicts

### Issue: Router appears to start but requests return unexpected responses

**Symptoms:**
- Running `ccrouter start` succeeds
- Requests to the router return unexpected responses (e.g., JSON errors from a different service)
- SSE events are malformed or missing event type lines

**Root Cause:**
Another service is already listening on the configured port (default: 8081). The router's HTTP server starts but can't bind to the port, so requests go to the other service.

**Diagnosis:**
```bash
# Check what process is listening on the port
lsof -i :8081
# or
netstat -an | grep 8081
# or
lsof -nP -iTCP -sTCP:LISTEN | grep 8081
```

If you see something like:
```
COMMAND    PID   USER   FD   TYPE  DEVICE SIZE/OFF NODE NAME
node     77308 avextk   13u  IPv6  ...   TCP *:8081 (LISTEN)
```

This indicates a Node.js server (or other service) is using the port.

**Solution:**
1. **Stop the conflicting service:**
   ```bash
   kill <PID>
   # e.g., kill 77308
   ```

2. **Or use a different port:**
   ```bash
   ccrouter start --port 8082
   ```

3. **Or configure a different default port in your config:**
   ```json
   {
     "server": {
       "port": 8082,
       "host": "localhost"
     }
   }
   ```

**Prevention:**
To avoid port conflicts in the future:
- Use a unique port for development environments
- Check for running instances with `ccrouter status` before starting
- Consider using environment-specific ports (e.g., 8081 for dev, 8082 for staging)

## Malformed SSE Events ("[object Object]" responses)

### Issue: Claude Code shows "[object Object]" instead of actual responses

**Symptoms:**
- Using `ccrouter code` with GLM or other providers
- Responses contain text like "[object Object]" instead of actual AI responses
- SSE events appear to be truncated or malformed

**Root Cause:**
This was caused by port conflicts (see above) where a different server was handling requests. The router code itself correctly handles SSE events with proper event type lines.

**Expected SSE Format:**
```
event: message_start
data: {"type":"message_start",...}

event: content_block_delta
data: {"type":"content_block_delta",...}

event: message_stop
data: {"type":"message_stop"}
```

**Diagnosis:**
```bash
# Test the router directly with curl
curl -s -N -X POST http://localhost:8081/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"glm-4.7","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Say hello"}]}'
```

Look for proper `event:` lines preceding each `data:` line.

**Solution:**
Follow the port conflict resolution steps above. Once the correct router process is handling requests, SSE events will include proper event types.

## GLM Provider Issues

### Issue: "ping" events in GLM responses

**Information:**
GLM (BigModel) API sends periodic `ping` events to keep the connection alive. These are automatically filtered out by the router as they are not part of the Anthropic SSE format.

**Log message:**
```
[STREAM] Filtering out non-Anthropic event: ping
```

This is normal behavior and not an error. The router correctly filters these events before passing responses to clients.

## Debugging Mode

### Enable Request/Response Logging

The router includes a logging interceptor that logs all requests and responses. This is enabled by default when using the `start` command.

**View logs in real-time:**
```bash
# Logs are written to stdout/stderr of the router process
ccrouter start 2>&1 | tee router.log
```

**Common log patterns:**
```
[REQUEST] Model: glm-4.7, Stream: true, Messages: 1, MaxTokens: 50
[ROUTE] Detected: default, Targets: 2
[STREAM] Starting stream to bigmodel/glm-4.7
[STREAM] Filtering out non-Anthropic event: ping
[STREAM] Stream completed with bigmodel/glm-4.7, fallbacks: 0
```

### Check Router Status

```bash
# Show all running instances
ccrouter status

# Output:
# INSTANCE ID           PID    PORT   STATUS   CONFIG  UPTIME
# inst_20260224_233233  42381  8081   running  global  5m30s
```

### Clean Up Stale Instances

```bash
# Remove dead/stale instance files
ccrouter clean
```

## Configuration Issues

### Issue: Provider not found

**Symptoms:**
```
Error: provider not found: xyz
```

**Solution:**
Check your configuration file includes the provider:
```json
{
  "providers": {
    "bigmodel": {
      "apiKey": "...",
      "baseURL": "https://open.bigmodel.cn/api/anthropic",
      "models": ["glm-4.7"]
    }
  }
}
```

### Issue: Transformer not found

**Symptoms:**
```
Error: transformer not found: xyz
```

**Solution:**
Transformers are registered based on provider names. Valid transformer names:
- `anthropic` (default)
- `openai`
- `openrouter`
- `gemini`
- `glm_anthropic`

Use a supported transformer name or omit the `transformer` field to use the default (provider name).

## Performance Issues

### Issue: Slow streaming responses

**Potential causes:**
1. **Network latency:** Check your connection to the provider's API
2. **Provider rate limits:** Some providers have rate limits that may cause delays
3. **Interceptor overhead:** Response interceptors add processing time

**Diagnosis:**
```bash
# Check timing in logs
[STREAM] Starting stream to bigmodel/glm-4.7
# ... time passes ...
[STREAM] Stream completed with bigmodel/glm-4.7
```

**Solution:**
- Use geographically closer provider endpoints
- Check provider status for outages
- Remove unnecessary interceptors

### Issue: High memory usage

**Potential causes:**
1. **Large streaming buffers:** Long-lived streaming connections
2. **Usage tracking:** Large buffers in the usage tracker

**Diagnosis:**
```bash
# Check memory usage
ps aux | grep ccrouter
```

**Solution:**
- Reduce usage tracker buffer size in configuration
- Regularly restart the router (`ccrouter restart`)

## ConnectionRefused Error

### Issue: `ccrouter code` fails with "Unable to connect to API (ConnectionRefused)"

**Symptoms:**
- Running `ccrouter code` immediately shows connection error
- Claude Code cannot connect to the router
- Error appears right after startup

**Status:** **Fixed in version 0.4.0+**

This was caused by a race condition in server startup where the `Start()` method returned before the server was actually listening on the port. This has been fixed by implementing a server readiness guarantee using explicit listener creation.

**If you still encounter this issue:**

1. **Check for port conflicts:**
   ```bash
   lsof -i :8081
   ```

2. **Try a different port:**
   ```bash
   ccrouter code --port 8082
   ```

3. **Check the router is actually starting:**
   ```bash
   ccrouter start
   ```

4. **See the troubleshooting steps above** for connection refused errors.

## Thinking Block and Content Validation Errors (CRITICAL)

### Issue: "Invalid input: expected string, received array" from OpenRouter

**Status:** **Fixed** (commits fa1d7ad through Mar 11 fixes)

This error went through three rounds of fixes as different edge cases surfaced.

#### Round 1 — Single thinking blocks during failover (commit fa1d7ad)

**Trigger:** GLM returns `[{thinking}]` in assistant messages. On failover to OpenRouter, the single-element array is rejected.

**Fix:** OpenRouter transformer now always normalizes thinking blocks, adding a text block with `" "` to produce `[{thinking}, {text: " "}]`.

#### Round 2 — Multiple thinking blocks (Mar 11 fix)

**Trigger:** After multiple GLM responses, conversation history contains `[{thinking}, {thinking}, {thinking}]`. The normalization only handled exactly-one-thinking-block at index 0.

**Fix:** Expanded normalization to detect content with ONLY thinking blocks (regardless of count or position), then add a text block.

#### Round 3 — Non-thinking single-element content (Mar 11 fix)

**Trigger:** Single image blocks, tool_result blocks, or mixed non-text content also produce single-element arrays that fail OpenRouter validation.

**Fix:** Generic `normalizeSingleElementContent()` replaces thinking-specific logic. Any single-element content without a text block gets a text block appended.

Additionally, `validateAndRepairBlocks()` handles corrupted blocks:

```go
func validateAndRepairBlocks(req *anthropic.Request) {
    for i := range req.Messages {
        for j := range req.Messages[i].Content {
            block := &req.Messages[i].Content[j]

            // Validate image blocks — handle both nil Source and empty Data
            if block.Type == "image" {
                if block.Source == nil || block.Source.Data == "" {
                    block.Type = "text"
                    block.Text = "[Image: data unavailable]"
                    block.Source = nil
                }
            }

            // Validate thinking blocks
            if block.Type == "thinking" && block.Thinking == "" {
                block.Type = "text"
                block.Text = "[Thinking: content unavailable]"
            }
        }
    }
}
```

**Diagnosis:**
```bash
grep "expected string, received array" ~/.cc-modelrouter/logs/*.log
grep "invalid_union" ~/.cc-modelrouter/logs/*.log
```

**Related fixes also applied:**
- State corruption across failover — deep copying in each transformer's `PrepareRequest()`
- Signature field validation — `*string` pointer type distinguishes omit vs empty
- User message thinking blocks — converted to text via `convertUserThinkingToText`

**See Also:**
- [Transformer documentation](transformers.md#thinking-block-handling-critical)

---

## State Corruption in Failover Scenarios

### Issue: Request modifications affect subsequent provider attempts

**Status:** **Fixed with Deep Copying**

**Symptoms:**
- First provider attempt succeeds but modifies the request
- Second provider attempt fails with validation errors
- Errors like "expected string, received array" on second attempt

**Root Cause:**
When transformers modify the request in-place (e.g., adding text blocks to thinking blocks), these modifications persist across failover attempts. The second provider receives a modified request that doesn't match its expected format.

**Solution:**
Transformers now create deep copies of the request before modification:

```go
var reqCopy anthropic.Request
reqJSON, err := json.Marshal(req)
if err != nil {
    return nil, fmt.Errorf("failed to marshal request for deep copy: %w", err)
}
if err := json.Unmarshal(reqJSON, &reqCopy); err != nil {
    return nil, fmt.Errorf("failed to unmarshal request deep copy: %w", err)
}
```

This ensures each provider receives an independent copy of the original request.

---

## BigModel Error 1213

### Issue: BigModel API returns error code 1213

**Symptoms:**
- Requests to BigModel/GLM provider fail with HTTP 400
- Error response contains code `1213`
- Streaming connections terminate unexpectedly

**Root Cause:**
BigModel API error 1213 is related to connection handling. This can occur when HTTP keep-alive connections become stale or when the server rejects requests on reused connections.

**Solution:**
Add `"disableKeepAlives": true` to the provider configuration:

```json
{
  "bigmodel": {
    "apiKey": "${CCROUTER_BIGMODEL_API_KEY}",
    "baseURL": "https://open.bigmodel.cn/api/anthropic",
    "models": ["glm-4.7", "glm-5-turbo"],
    "disableKeepAlives": true
  }
}
```

This forces a fresh TCP connection for each request, avoiding stale connection issues.

**Status:** Fixed with enhanced error handling and `disableKeepAlives` provider option.

---

## Getting Help

If you encounter issues not covered here:

1. **Enable debug logging:** Look for log messages that indicate where the failure occurs
2. **Test the provider directly:** Use curl to test the provider API without the router
3. **Check the logs:** Router logs often contain detailed error messages
4. **Review configuration:** Ensure all required fields are present and correct
5. **Check for thinking blocks:** If using extended thinking, be aware of provider-specific validation requirements
6. **File an issue:** Include logs, configuration, and steps to reproduce
