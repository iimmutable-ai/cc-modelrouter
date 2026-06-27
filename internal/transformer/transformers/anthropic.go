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

// Helper functions for string pointer operations

// strPtr returns a pointer to the given string.
// Used for setting Signature fields where we need to distinguish between:
// - nil (omit field)
// - &"" (include empty string)
// - &"value" (include actual value)
func strPtr(s string) *string {
	return &s
}

// isWhitespacePtr returns true if the string pointer is nil or points to whitespace-only string.
func isWhitespacePtr(s *string) bool {
	if s == nil {
		return true
	}
	return strings.TrimSpace(*s) == ""
}

// strPtrValue returns the string value, or empty string if pointer is nil.
func strPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// convertUserThinkingToText converts thinking blocks in user messages to text blocks.
//
// When Claude Code resends previous assistant responses as context, thinking blocks
// may appear in user messages. These need to be converted to text blocks to prevent
// "expected string, received array" errors with providers like OpenRouter.
//
// This function preserves the thinking content by wrapping it in <thinking> tags,
// allowing the content to be properly marshaled as part of the message text.
func convertUserThinkingToText(req *anthropic.Request) {
	for i := range req.Messages {
		// Only process user messages
		if req.Messages[i].Role != anthropic.RoleUser {
			continue
		}

		hasThinking := false
		for _, block := range req.Messages[i].Content {
			if block.Type == "thinking" || block.Type == "redacted_thinking" {
				hasThinking = true
				break
			}
		}

		// If no thinking blocks, skip this message
		if !hasThinking {
			continue
		}

		// Convert thinking blocks to text blocks
		var newContent []anthropic.ContentBlock
		for _, block := range req.Messages[i].Content {
			if block.Type == "thinking" {
				// Convert thinking to text with XML-style tags
				newContent = append(newContent, anthropic.ContentBlock{
					Type: "text",
					Text: "<thinking>" + block.Thinking + "</thinking>",
				})
			} else if block.Type == "redacted_thinking" {
				// Convert redacted_thinking to text placeholder
				newContent = append(newContent, anthropic.ContentBlock{
					Type: "text",
					Text: "<redacted_thinking/>",
				})
			} else {
				// Preserve other block types unchanged
				newContent = append(newContent, block)
			}
		}
		req.Messages[i].Content = newContent
	}
}

// stripAssistantThinkingBlocks removes thinking and redacted_thinking blocks from
// assistant messages. This is a workaround for BigModel's Anthropic-compatible endpoint
// which returns error 1213 when assistant messages contain thinking blocks.
//
// BigModel does support thinking via native reasoning_content format, but their
// Anthropic endpoint doesn't properly convert Anthropic thinking blocks. A degraded
// response (without thinking context) is better than 100% failure.
func stripAssistantThinkingBlocks(req *anthropic.Request) {
	for i := range req.Messages {
		if req.Messages[i].Role != anthropic.RoleAssistant {
			continue
		}

		hasThinking := false
		for _, block := range req.Messages[i].Content {
			if block.Type == "thinking" || block.Type == "redacted_thinking" {
				hasThinking = true
				break
			}
		}

		if !hasThinking {
			continue
		}

		var filtered []anthropic.ContentBlock
		for _, block := range req.Messages[i].Content {
			if block.Type != "thinking" && block.Type != "redacted_thinking" {
				filtered = append(filtered, block)
			}
		}
		req.Messages[i].Content = filtered
	}
}

// normalizeSingleElementContent ensures ANY content array without text blocks
// is normalized to prevent provider validation errors.
// This handles: single thinking, multiple thinking, single image, single tool_result,
// thinking + image, etc. for ALL message roles.
//
// CRITICAL: This function processes ALL message roles (user, assistant, system),
// not just assistant messages. Previously, only assistant messages were processed,
// causing user messages with single thinking blocks to fail OpenRouter validation
// with "expected string, received array" errors.
//
// The key insight from MessageContent.MarshalJSON():
// - Single text block → marshals as STRING (valid)
// - Anything else (single element or array without text) → marshals as ARRAY (fails OpenRouter validation)
//
// This function ensures ALL messages have at least one text block,
// making the content a multi-element array that passes validation.
func normalizeSingleElementContent(req *anthropic.Request) {
	for i := range req.Messages {
		// CRITICAL FIX: Process ALL message roles, not just assistant
		// User messages with thinking blocks from conversation history
		// were causing "expected string, received array" errors
		content := req.Messages[i].Content
		if len(content) == 0 {
			continue
		}

		// Check if content has a valid text block (non-empty text)
		// CRITICAL FIX: Also check if text is non-empty
		// Empty text "" or whitespace-only text causes
		// "messages: text content blocks must be non-empty" errors
		hasValidTextBlock := false
		for _, block := range content {
			if block.Type == "text" && block.Text != "" && len(strings.TrimSpace(block.Text)) > 0 {
				hasValidTextBlock = true
				break
			}
		}

		// If no valid text block, add one to ensure valid multi-element array
		// This handles: single thinking, multiple thinking, single image,
		// single tool_result, thinking + image, empty text blocks, etc.
		if !hasValidTextBlock {
			req.Messages[i].Content = append(content, anthropic.ContentBlock{
				Type: "text",
				Text: "[thinking context removed for provider compatibility]",
			})
		}
	}
}

// normalizeThinkingBlockMessages is a wrapper for normalizeSingleElementContent
// for backward compatibility with existing code.
func normalizeThinkingBlockMessages(req *anthropic.Request) {
	normalizeSingleElementContent(req)
}

// validateAndRepairBlocks checks for blocks with missing/invalid required fields
// and repairs them where possible. This prevents "expected string, received undefined"
// errors at paths like [N, "data"].
func validateAndRepairBlocks(req *anthropic.Request) {
	for i := range req.Messages {
		for j := range req.Messages[i].Content {
			block := &req.Messages[i].Content[j]

			// Validate image blocks - require source.data
			// FIX: Handle BOTH nil Source AND empty Data cases
			// Previously the check was: block.Type == "image" && block.Source != nil
			// This skipped blocks where Source was nil, causing [3, "data"] errors
			if block.Type == "image" {
				if block.Source == nil || block.Source.Data == "" {
					// Replace invalid image with text placeholder
					block.Type = "text"
					block.Text = "[Image: data unavailable]"
					block.Source = nil
				}
			}

			// Validate thinking blocks - require thinking content
			if block.Type == "thinking" && block.Thinking == "" {
				block.Type = "text"
				block.Text = "[Thinking: content unavailable]"
			}

			// Validate redacted_thinking blocks - require data field
			if block.Type == "redacted_thinking" && block.Data == "" {
				block.Type = "text"
				block.Text = "[Redacted thinking]"
			}

			// Validate document blocks - require document source
			// Similar to image blocks, nil DocumentSource can cause validation errors
			if block.Type == "document" {
				if block.DocumentSource == nil {
					block.Type = "text"
					block.Text = "[Document: source unavailable]"
					block.DocumentSource = nil
				}
			}
		}
	}
}

// AnthropicTransformer is a pass-through transformer for Anthropic-compatible APIs.
type AnthropicTransformer struct {
	*transformer.BaseTransformer
}

// NewAnthropicTransformer creates a new Anthropic transformer.
func NewAnthropicTransformer() *AnthropicTransformer {
	return &AnthropicTransformer{
		BaseTransformer: transformer.NewBaseTransformer("anthropic"),
	}
}

// Endpoint returns the API endpoint path.
func (t *AnthropicTransformer) Endpoint() string {
	return "/v1/messages"
}

// PrepareRequest converts Anthropic request to provider HTTP request (pass-through).
func (t *AnthropicTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	// Create a copy to avoid modifying the original
	reqCopy := *req
	reqCopy.Model = model

	// CRITICAL: Process in order to ensure correct content format
	// 1. First, convert user thinking blocks to text (prevents format issues)
	// 2. Then, normalize assistant thinking blocks (ensures multi-element arrays)
	convertUserThinkingToText(&reqCopy)
	// NOTE: Anthropic API accepts single thinking blocks without normalization
	// We do NOT call normalizeThinkingBlockMessages for direct Anthropic API
	// because it creates an invalid format [thinking, text(" ")] that the API rejects

	// CRITICAL: Normalize thinking block signatures for Anthropic compatibility
	// Anthropic's API rejects whitespace-only signatures (e.g., " ", "\t").
	// We set whitespace-only signatures to nil to allow MarshalJSON to omit them entirely.
	// This is different from OpenRouter/GLM providers which require the signature field to be present.
	for i := range reqCopy.Messages {
		for j := range reqCopy.Messages[i].Content {
			if reqCopy.Messages[i].Content[j].Type == "thinking" {
				// Strip whitespace-only signatures - set to nil so MarshalJSON omits the field
				if isWhitespacePtr(reqCopy.Messages[i].Content[j].Signature) {
					reqCopy.Messages[i].Content[j].Signature = nil // Omit field entirely
				}
			}
		}
	}

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// DIAGNOSTIC: Log request body details for debugging thinking signature issues
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
func (t *AnthropicTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var anthropicResp anthropic.Response
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &anthropicResp, nil
}

// SupportsStreaming returns true.
func (t *AnthropicTransformer) SupportsStreaming() bool {
	return true
}

// TransformStreamEvent passes through Anthropic events unchanged.
func (t *AnthropicTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	return []transformer.SSEEvent{*event}, nil
}
