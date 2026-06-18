# Transformers Guide

Transformers convert requests and responses between Anthropic format and provider-specific API formats.

## Transformer Interface

```go
type Transformer interface {
    // Name returns the transformer name (used for registry lookup)
    Name() string

    // Endpoint returns the API endpoint path for this transformer
    Endpoint() string

    // PrepareRequest converts an Anthropic request to a provider-specific HTTP request
    PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error)

    // ParseResponse converts a provider HTTP response to Anthropic format
    ParseResponse(resp *http.Response) (*anthropic.Response, error)

    // SupportsStreaming returns true if this transformer supports streaming
    SupportsStreaming() bool

    // TransformStreamEvent converts a provider SSE event to Anthropic SSE events
    TransformStreamEvent(event *SSEEvent) ([]SSEEvent, error)
}

// SSEEvent represents a complete server-sent event with type and data.
type SSEEvent struct {
    EventType string  // SSE event type (e.g., "message_start", "content_block_delta")
    Data      []byte  // Raw JSON data payload
}
```

## Built-in Transformers

### AnthropicTransformer

Pass-through transformer for Anthropic-native APIs.

**Use case:** Direct Anthropic API or Anthropic-compatible endpoints.

**Request format:** Unchanged (Anthropic native)
**Auth:** `x-api-key` header

```go
// Request
POST /v1/messages
Headers:
  x-api-key: <api-key>
  anthropic-version: 2023-06-01
```

### OpenAI Transformer

Converts to OpenAI chat completions format for OpenAI-compatible APIs.

**Use case:** OpenAI-compatible APIs (OpenAI, Qwen, etc.)

**Request format:** OpenAI-compatible
**Auth:** `Authorization: Bearer` header

```go
// Request
POST /chat/completions
Headers:
  Authorization: Bearer <api-key>
Body:
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "..."}],
  "max_tokens": 4096
}
```

**Note:** Use this transformer for:
- Direct OpenAI API
- Any OpenAI-compatible API

### OpenRouterTransformer

Wraps the Anthropic transformer with OpenRouter-specific handling for signature preservation and thinking block normalization.

**Use case:** OpenRouter API (all models including Anthropic)

**Request format:** Anthropic Messages API format
**Auth:** `x-api-key` header

```go
// Request
POST /v1/messages
Headers:
  x-api-key: <api-key>
  anthropic-version: 2023-06-01
Body:
{
  "model": "anthropic/claude-sonnet-4",
  "messages": [{"role": "user", "content": "..."}],
  "max_tokens": 4096
}
```

**Key differences from AnthropicTransformer:**
- Always normalizes thinking blocks (adds text block to single thinking-only content)
- Preserves signature fields (sets to empty string `""` instead of omitting)
- Required because OpenRouter validates strictly on these fields

### GeminiTransformer

Converts to Google Gemini API format.

**Use case:** Google Gemini API

**Request format:** Gemini native (`contents`/`parts`)
**Auth:** Query parameter `key=`

```go
// Request
POST /models/{model}:generateContent?key={api-key}
Body:
{
  "contents": [
    {
      "role": "user",
      "parts": [{"text": "..."}]
    }
  ],
  "systemInstruction": {
    "parts": [{"text": "..."}]
  },
  "tools": [...]
}
```

**Key transformations:**
- `messages` → `contents` with `parts`
- `system` → `systemInstruction`
- `assistant` role → `model` role
- Tools → `functionDeclarations`
- Images → `inlineData`

### Aliyun DashScope (alicloud)

The alicloud preset uses Anthropic-compatible format. Use the **GLM Transformer** (see below).

**Configuration:**
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

### GLMTransformer

Pass-through for Anthropic-compatible GLM API.

**Use case:** Zhipu BigModel, Aliyun DashScope, MiniMax

**Request format:** Anthropic-compatible
**Auth:** `x-api-key` header

```go
// Request
POST /v1/messages
Headers:
  x-api-key: <api-key>
  anthropic-version: 2023-06-01
Body: (Anthropic format)
```

## Transformer Registry

Transformers are registered at startup in `internal/cli/start.go` and `internal/cli/code.go`:

```go
registry := transformer.NewRegistry()
registry.Register(transformers.NewAnthropicTransformer())
registry.Register(transformers.NewGLMAnthropicTransformer())
registry.Register(transformers.NewOpenRouterTransformer())
registry.Register(transformers.NewOpenAITransformer())
registry.Register(transformers.NewGeminiTransformer())
```

**Lookup:**
```go
t, err := registry.Get("gemini")
if err != nil {
    // Transformer not found
}
```

## Creating a Custom Transformer

### 1. Implement the Interface

```go
package transformers

type MyTransformer struct{}

func NewMyTransformer() *MyTransformer {
    return &MyTransformer{}
}

func (t *MyTransformer) Name() string {
    return "myprovider"
}

func (t *MyTransformer) Endpoint() string {
    return "/v1/messages"  // or provider-specific endpoint
}

func (t *MyTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
    // Convert Anthropic request to provider format
    providerReq := convertRequest(req, model)

    body, err := json.Marshal(providerReq)
    if err != nil {
        return nil, err
    }

    httpReq, err := http.NewRequest("POST", baseURL+"/endpoint", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }

    // Set appropriate headers
    httpReq.Header.Set("Authorization", "Bearer "+apiKey)
    httpReq.Header.Set("Content-Type", "application/json")

    return httpReq, nil
}

func (t *MyTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
    if resp.StatusCode >= 400 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
    }

    var providerResp ProviderResponse
    if err := json.NewDecoder(resp.Body).Decode(&providerResp); err != nil {
        return nil, err
    }

    return convertToAnthropic(&providerResp), nil
}

func (t *MyTransformer) SupportsStreaming() bool {
    return true // or false
}

func (t *MyTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
    // Convert streaming SSE event format
    return []transformer.SSEEvent{*event}, nil // or transform
}
```

### 2. Register the Transformer

```go
// In internal/cli/start.go and internal/cli/code.go
registry.Register(transformers.NewMyTransformer())
```

## Format Conversion Patterns

### Message Content

**Anthropic:**
```json
{
  "content": [
    {"type": "text", "text": "Hello"},
    {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}
  ]
}
```

**OpenAI-compatible:**
```json
{
  "content": "Hello"
}
```

**Gemini:**
```json
{
  "parts": [
    {"text": "Hello"},
    {"inlineData": {"mimeType": "image/png", "data": "..."}}
  ]
}
```

### Tools

**Anthropic:**
```json
{
  "tools": [
    {
      "name": "get_weather",
      "description": "Get weather",
      "input_schema": {"type": "object", "properties": {...}}
    }
  ]
}
```

**OpenAI-compatible:**
```json
{
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get weather",
        "parameters": {"type": "object", "properties": {...}}
      }
    }
  ]
}
```

**Gemini:**
```json
{
  "tools": [
    {
      "functionDeclarations": [
        {
          "name": "get_weather",
          "description": "Get weather",
          "parameters": {...}
        }
      ]
    }
  ]
}
```

### Tool Use in Response

**Anthropic:**
```json
{
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_123",
      "name": "get_weather",
      "input": {"location": "SF"}
    }
  ]
}
```

**OpenAI-compatible:**
```json
{
  "choices": [{
    "message": {
      "tool_calls": [{
        "id": "call_123",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\": \"SF\"}"
        }
      }]
    }
  }]
}
```

**Gemini:**
```json
{
  "candidates": [{
    "content": {
      "parts": [{
        "functionCall": {
          "name": "get_weather",
          "args": {"location": "SF"}
        }
      }]
    }
  }]
}
```

## File Locations

```
internal/transformer/
├── interface.go        # Transformer interface
├── registry.go         # Transformer registry
├── base.go             # Base types
└── transformers/       # Transformer implementations
    ├── anthropic.go    # Anthropic (pass-through)
    ├── openai.go      # OpenAI-compatible
    ├── openrouter.go  # OpenRouter (handles signature preservation)
    ├── gemini.go      # Gemini native format
    └── glm_anthropic.go # GLM Anthropic-compatible
```

**Note:** Transformers are registered in `internal/cli/start.go` and `internal/cli/code.go`.

---

## Thinking Block Handling (CRITICAL)

### Overview

Thinking blocks are a special content block type used by Anthropic's extended thinking feature. Different providers have different validation requirements for thinking blocks, making correct handling critical.

### Provider Signature Requirements

| Provider | Signature Requirement | Notes |
|----------|---------------------|-------|
| **Direct Anthropic API** | Omit when empty | Rejects whitespace-only signatures like `" "` |
| **OpenRouter Anthropic models** | Must be present | Requires field even if empty string |
| **GLM providers (Aliyun, BigModel)** | Must be present | Accepts empty string value |
| **Other OpenRouter models** | Must be present | Accepts empty or whitespace values |

### The Signature Field Solution

The implementation uses pointer types to distinguish between omitting and including the field:

```go
type ContentBlock struct {
    Signature *string `json:"signature,omitempty"` // Pointer distinguishes omit vs empty string
}
```

This allows:
- `nil` → omit field (for Anthropic API)
- `&""` → include empty string (for OpenRouter, GLM)
- `&"value"` → include actual value

### Content Normalization

**User Messages with Thinking Blocks:**
- Claude Code may resend previous assistant responses as user messages
- These can contain thinking blocks that need conversion
- **Solution:** Convert thinking blocks to text blocks wrapped in `<thinking>` tags

**Assistant Messages with Single Thinking Blocks:**
- Some providers reject single-element arrays for content
- **Solution:** Add a minimal text block with single space to make it multi-element

**Failover State Corruption:**
- Transformers must deep copy requests before modification
- Otherwise, modifications affect subsequent provider attempts
- **Solution:** JSON marshal/unmarshal to create independent copies

### Transformer-Specific Handling

#### AnthropicTransformer
```go
// Strip whitespace-only signatures (set to nil to omit field)
if isWhitespacePtr(signature) {
    signature = nil  // Will be omitted by MarshalJSON
}
// Do NOT normalize single thinking blocks (Anthropic accepts them)
```

#### OpenRouterTransformer

**CRITICAL: OpenRouter requires different handling than direct Anthropic API**

```go
// CRITICAL: Always normalize thinking blocks for OpenRouter
// Both Anthropic and non-Anthropic models via OpenRouter require multi-element arrays
// to prevent "expected string, received array" validation errors
normalizeThinkingBlockMessages(&reqCopy)

// Handle signatures based on target model
// OpenRouter Anthropic models require signature field to be PRESENT (not omitted)
// Existing signatures from previous provider responses MUST be cleared
if isAnthropicModel {
    // ALWAYS clear signature for OpenRouter Anthropic models
    // Setting to empty string ensures the field is present (required by OpenRouter)
    // Clears any existing signature from previous provider responses
    reqCopy.Messages[i].Content[j].Signature = strPtr("")
} else {
    // Non-Anthropic models: ensure signature field is present
    if reqCopy.Messages[i].Content[j].Signature == nil || isWhitespacePtr(reqCopy.Messages[i].Content[j].Signature) {
        reqCopy.Messages[i].Content[j].Signature = strPtr("")  // Include empty string
    }
}
```

**Key Differences from Direct Anthropic API:**

1. **Content Normalization:**
   - Direct Anthropic API: Accepts single thinking blocks without normalization
   - OpenRouter (all models): **Requires normalization** - single thinking blocks cause validation errors

2. **Signature Handling:**
   - Direct Anthropic API: Omit signature field when empty (set to `nil`)
   - OpenRouter Anthropic: Include signature field even when empty (set to `&""`)

3. **Failover Scenarios:**
   - When GLM or other providers return thinking blocks in assistant messages
   - These messages are sent to OpenRouter during failover
   - OpenRouter's strict validation rejects the single-element array format
   - Normalization adds a text block, creating a valid multi-element array

#### GLMAnthropicTransformer
```go
// Always include signature field
if signature == nil {
    signature = strPtr("")  // Include empty string
}
// Normalize thinking blocks
normalizeThinkingBlockMessages(&reqCopy)
```

### Testing Thinking Block Handling

```go
// Test case: OpenRouter Anthropic model with thinking block
req := &anthropic.Request{
    Messages: []anthropic.Message{
        {
            Role: "assistant",
            Content: anthropic.MessageContent{
                {Type: "thinking", Thinking: "..."},
            },
        },
    },
}

// After transformer processing:
// - Signature should be &"" (include empty string)
// - Content should remain single-element array (no normalization)
// - JSON should include "signature": ""
```

---

## Common Transformer Pitfalls

### 1. Modifying Requests In-Place

**Wrong:**
```go
func (t *MyTransformer) PrepareRequest(req *anthropic.Request, ...) {
    req.Messages[0].Content = append(req.Messages[0].Content, newBlock)
    // This modifies the original request!
}
```

**Right:**
```go
func (t *MyTransformer) PrepareRequest(req *anthropic.Request, ...) {
    var reqCopy anthropic.Request
    json.Unmarshal(json.Marshal(req), &reqCopy)
    reqCopy.Messages[0].Content = append(...)
    // Work with copy
}
```

### 2. Not Handling Provider-Specific Validation

**Wrong:**
```go
// Treat all providers the same
if block.Type == "thinking" {
    block.Signature = ""  // Will be omitted - breaks OpenRouter!
}
```

**Right:**
```go
// Handle provider-specific requirements
if isAnthropicModel {
    block.Signature = nil  // Omit for Anthropic API
} else {
    block.Signature = strPtr("")  // Include for others
}
```

### 3. Forgetting to Set GetBody for HTTP Requests

**Wrong:**
```go
httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
// Body can only be read once - retries will fail!
```

**Right:**
```go
bodyCopy := make([]byte, len(body))
copy(bodyCopy, body)
httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
httpReq.GetBody = func() (io.ReadCloser, error) {
    return io.NopCloser(bytes.NewReader(bodyCopy)), nil
}
```
