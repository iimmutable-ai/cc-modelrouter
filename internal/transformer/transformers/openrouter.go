package transformers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// OpenRouterTransformer is a specialized transformer for OpenRouter.
//
// OpenRouter provides access to multiple model providers with different validation
// requirements for thinking blocks:
//
// - Anthropic models (anthropic/*): CRITICAL - Require signature field to be PRESENT as a string
//   Cannot omit the field even when empty (causes "expected string, received undefined" error)
//   Additionally, existing signatures from previous provider responses MUST be cleared
//   because they are cryptographically invalid and cause "Invalid signature" errors
// - Other models (google/*, etc.): May require signature field to be present
//
// This transformer implements model-specific signature handling based on the target model.
type OpenRouterTransformer struct {
	*AnthropicTransformer
}

// NewOpenRouterTransformer creates a new OpenRouter-specific transformer.
func NewOpenRouterTransformer() *OpenRouterTransformer {
	return &OpenRouterTransformer{
		AnthropicTransformer: NewAnthropicTransformer(),
	}
}

// Name returns the transformer name.
func (t *OpenRouterTransformer) Name() string {
	return "openrouter"
}

// Endpoint returns the API endpoint path (same as Anthropic).
func (t *OpenRouterTransformer) Endpoint() string {
	return t.AnthropicTransformer.Endpoint()
}

// PrepareRequest converts Anthropic request to provider HTTP request with OpenRouter-specific handling.
//
// For OpenRouter, we handle signatures based on the target model:
// - Anthropic models (anthropic/*): CRITICAL - Always clear signature to empty string
//   This prevents "Invalid signature" errors when signatures from previous provider
//   responses (GLM, OpenAI, etc.) are included in the request
// - Other models: Ensure signature field is present with empty string if nil/whitespace
//
// Messages with only thinking blocks get a minimal text block to prevent validation errors.
//
// CRITICAL: This method creates a true deep copy of the request to prevent state corruption
// across provider failover attempts. A shallow copy would cause modifications to thinking
// blocks to affect subsequent provider requests, leading to 400 Bad Request errors.
func (t *OpenRouterTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
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

	// CRITICAL: Process in order to ensure correct content format
	// 1. First, convert user thinking blocks to text (prevents format issues)
	// 2. Then, normalize ALL assistant thinking blocks (required for OpenRouter validation)
	convertUserThinkingToText(&reqCopy)

	// CRITICAL FIX: Always normalize thinking blocks for OpenRouter
	// OpenRouter's validation is stricter than direct Anthropic API:
	// - OpenRouter rejects single-element content arrays for thinking blocks
	// - Requires multi-element arrays (e.g., [{thinking}, {text: " "}])
	// - This applies to BOTH Anthropic and non-Anthropic models via OpenRouter
	//
	// Why this is necessary:
	// - GLM (and other providers) return assistant messages with single thinking blocks
	// - These get added to conversation history
	// - When next request goes to OpenRouter Anthropic model, the thinking blocks
	//   in conversation history are in single-element array format
	// - OpenRouter's validation rejects this format with "expected string, received array"
	//
	// The fix: Always normalize ALL single-element content (not just thinking),
	// creating a multi-element array that passes validation.
	// Additionally, validate and repair blocks with missing required fields
	// to prevent "expected string, received undefined" errors.
	normalizeSingleElementContent(&reqCopy)
	validateAndRepairBlocks(&reqCopy)

	// Determine target model type for signature handling
	isAnthropicModel := strings.HasPrefix(model, "anthropic/")

	// Handle signatures based on target model
	// CRITICAL: OpenRouter Anthropic models require signature field to be PRESENT (not omitted)
	// CRITICAL: Existing signatures from previous provider responses MUST be cleared for Anthropic models
	// because they are cryptographically invalid for OpenRouter's validation and cause 400 errors.
	// Other models: Ensure signature field is present with empty string if nil or whitespace
	for i := range reqCopy.Messages {
		for j := range reqCopy.Messages[i].Content {
			if reqCopy.Messages[i].Content[j].Type == "thinking" {
				if isAnthropicModel {
					// CRITICAL FIX: ALWAYS clear signature for OpenRouter Anthropic models
					// - Setting to empty string ensures the field is present (required by OpenRouter)
					// - Clears any existing signature from previous provider responses (GLM, OpenAI, etc.)
					// - Existing signatures are cryptographically invalid and cause "Invalid signature" errors
					// - This is the only safe value that passes OpenRouter's validation
					reqCopy.Messages[i].Content[j].Signature = strPtr("")
				} else {
					// Non-Anthropic models: ensure signature field is present
					// Replace nil or whitespace-only signatures with empty string
					// Note: Non-empty, non-whitespace signatures are preserved for non-Anthropic models
					if reqCopy.Messages[i].Content[j].Signature == nil || isWhitespacePtr(reqCopy.Messages[i].Content[j].Signature) {
						reqCopy.Messages[i].Content[j].Signature = strPtr("") // Include empty string
					}
				}
			}
		}
	}

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// DIAGNOSTIC: Log request body for debugging
	bodyStr := string(body)
	if len(bodyStr) > 2000 {
		logging.StreamDebugf("[PROXY REQUEST BODY] Model: %s, Body (first 2000 chars): %s...", model, bodyStr[:2000])
	} else {
		logging.StreamDebugf("[PROXY REQUEST BODY] Model: %s, Body: %s", model, bodyStr)
	}

	// DIAGNOSTIC: Specifically log thinking blocks with signature info
	for i, msg := range reqCopy.Messages {
		for j, block := range msg.Content {
			if block.Type == "thinking" {
				sigValue := strPtrValue(block.Signature)
				if sigValue == "" {
					sigValue = "<EMPTY>"
				}
				logging.StreamDebugf("[THINKING BLOCK] Message[%d].Content[%d]: thinking=%d chars, signature=%s",
					i, j, len(block.Thinking), sigValue)
			}
		}
	}

	endpoint := baseURL
	if !bytes.HasSuffix([]byte(baseURL), []byte("/v1/messages")) {
		endpoint = baseURL + "/v1/messages"
	}

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

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("User-Agent", "cc-modelrouter/1.0")
	httpReq.Header.Set("Accept", "application/json")

	return httpReq, nil
}

// ParseResponse converts provider HTTP response to Anthropic response (pass-through).
func (t *OpenRouterTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	return t.AnthropicTransformer.ParseResponse(resp)
}

// SupportsStreaming returns true (OpenRouter supports streaming).
func (t *OpenRouterTransformer) SupportsStreaming() bool {
	return true
}

// TransformStreamEvent passes through Anthropic events unchanged.
func (t *OpenRouterTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	return t.AnthropicTransformer.TransformStreamEvent(event)
}
