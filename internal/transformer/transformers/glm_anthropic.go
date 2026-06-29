package transformers

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// GLMAnthropicTransformer is a specialized transformer for GLM providers (aliyun, bigmodel)
// that ensures the signature field is present in thinking blocks.
//
// GLM providers (Aliyun DashScope, BigModel) require the signature field to be
// present in thinking blocks, even when empty. This is different from Anthropic's
// API which rejects empty signature values.
//
// This transformer wraps the standard AnthropicTransformer and adds provider-specific
// request sanitization before marshaling.
type GLMAnthropicTransformer struct {
	*AnthropicTransformer
}

// NewGLMAnthropicTransformer creates a new GLM-specific Anthropic transformer.
func NewGLMAnthropicTransformer() *GLMAnthropicTransformer {
	return &GLMAnthropicTransformer{
		AnthropicTransformer: NewAnthropicTransformer(),
	}
}

// Name returns the transformer name.
func (t *GLMAnthropicTransformer) Name() string {
	return "glm-anthropic"
}

// Endpoint returns the API endpoint path (same as Anthropic).
func (t *GLMAnthropicTransformer) Endpoint() string {
	return t.AnthropicTransformer.Endpoint()
}

// PrepareRequest converts Anthropic request to provider HTTP request with GLM-specific handling.
//
// For GLM providers, we ensure that:
// 1. The signature field is present in thinking blocks (even when empty)
// 2. Assistant messages with only thinking blocks have at least one text block
//
// CRITICAL: This method creates a true deep copy of the request to prevent state corruption
// across provider failover attempts. A shallow copy would cause modifications to thinking
// blocks to affect subsequent provider requests, leading to 400 Bad Request errors.
func (t *GLMAnthropicTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	// Create a true deep copy using JSON marshal/unmarshal
	// This is necessary because shallow copying the Request struct shares the underlying
	// Message and ContentBlock arrays, causing state corruption when multiple providers
	// are attempted in sequence during failover.
	var reqCopy anthropic.Request
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request for deep copy: %w", err)
	}
	if err := json.Unmarshal(reqJSON, &reqCopy); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request deep copy: %w", err)
	}
	reqCopy.Model = model
	reqCopy.Stream = true // GLM always streams; stream=false causes error 1213

	// CRITICAL: Process in order to ensure correct content format
	// 1. Convert user thinking blocks to text (prevents format issues)
	// 2. Strip thinking blocks from assistant messages (BigModel Anthropic endpoint bug workaround)
	// 3. Normalize ALL single-element content (ensures multi-element arrays)
	// 4. Validate and repair blocks with missing required fields
	// 5. Truncate tool names exceeding GLM's 64-character limit
	convertUserThinkingToText(&reqCopy)
	stripAssistantThinkingBlocks(&reqCopy)
	normalizeSingleElementContent(&reqCopy)
	validateAndRepairBlocks(&reqCopy)
	truncateToolNames(&reqCopy)
	normalizeNullToolSchemas(&reqCopy)

	// GLM-specific: Ensure signature field is present for thinking blocks
	// We do this AFTER normalization to ensure signature is set
	// Note: With the pointer type, we can now set empty string to include the field
	// without using a single space workaround.
	for i := range reqCopy.Messages {
		for j := range reqCopy.Messages[i].Content {
			if reqCopy.Messages[i].Content[j].Type == "thinking" {
				// Set signature to empty string if nil (include field in JSON)
				if reqCopy.Messages[i].Content[j].Signature == nil {
					reqCopy.Messages[i].Content[j].Signature = strPtr("") // Include empty string
				}
			}
		}
	}

	// Marshal the request with GLM-specific handling
	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// DIAGNOSTIC: Log request body for debugging (with additional validation info)
	bodyStr := string(body)
	bodyHash := fmt.Sprintf("%x", sha256.Sum256(body))
	if len(bodyStr) > 2000 {
		logging.StreamDebugf("[PROXY REQUEST BODY] Model: %s, Size: %d bytes, Hash: %s..., Body (first 2000 chars): %s...", model, len(body), bodyHash[:16], bodyStr[:2000])
	} else {
		logging.StreamDebugf("[PROXY REQUEST BODY] Model: %s, Size: %d bytes, Hash: %s, Body: %s", model, len(body), bodyHash, bodyStr)
	}

	// Build endpoint
	endpoint := baseURL
	if !strings.HasSuffix(baseURL, "/v1/messages") {
		endpoint = baseURL + "/v1/messages"
	}

	logging.StreamDebugf("[GLM REQUEST] URL: %s, Headers: x-api-key=<redacted>, anthropic-version=2023-06-01, User-Agent=%s", endpoint, t.UserAgent())

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// CRITICAL: Set GetBody to enable retries
	// The HTTP client may retry the request if there's a network error.
	// Without GetBody, the body reader would be exhausted on retry causing
	// "http: ContentLength=X with Body length 0" errors.
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}

	// CRITICAL: Set explicit Content-Length to prevent any ambiguity
	httpReq.ContentLength = int64(len(body))

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("User-Agent", t.UserAgent())
	httpReq.Header.Set("Accept", "application/json")

	return httpReq, nil
}

// ParseResponse converts provider HTTP response to Anthropic response (pass-through).
func (t *GLMAnthropicTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	return t.AnthropicTransformer.ParseResponse(resp)
}

// SupportsStreaming returns true (GLM supports streaming).
func (t *GLMAnthropicTransformer) SupportsStreaming() bool {
	return true
}

// TransformStreamEvent passes through Anthropic events unchanged.
func (t *GLMAnthropicTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	return t.AnthropicTransformer.TransformStreamEvent(event)
}

// maxToolNameLength is the maximum tool name length supported by GLM providers.
// Aliyun DashScope and BigModel both enforce a 64-character limit on tool names.
// Claude Code sends tools like "mcp__plugin_everything-claude-code_github__create_pull_request_review"
// (70+ chars) which exceeds this limit.
const maxToolNameLength = 64

// truncateToolNames truncates tool names that exceed GLM's 64-character limit.
// Names are truncated to 57 chars + "_" + 6-char hex hash suffix (total 64) to preserve
// uniqueness when multiple long tool names share the same prefix.
func truncateToolNames(req *anthropic.Request) {
	if len(req.Tools) == 0 {
		return
	}
	for i := range req.Tools {
		if len(req.Tools[i].Name) > maxToolNameLength {
			originalName := req.Tools[i].Name
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(originalName)))[:6]
			req.Tools[i].Name = originalName[:57] + "_" + hash
			logging.StreamDebugf("[GLM] Truncated tool name (%d chars): %s -> %s", len(originalName), originalName, req.Tools[i].Name)
		}
	}
}

// normalizeNullToolSchemas replaces nil InputSchema with a minimal object schema.
// BigModel's GLM-Anthropic API rejects "input_schema": null with HTTP 422
// ("Input should be a valid dictionary"). Claude Code's internal web_search
// tool legitimately sends null schemas, so coerce to {"type":"object"}.
func normalizeNullToolSchemas(req *anthropic.Request) {
	for i := range req.Tools {
		if req.Tools[i].InputSchema == nil {
			req.Tools[i].InputSchema = map[string]any{"type": "object"}
		}
	}
}
