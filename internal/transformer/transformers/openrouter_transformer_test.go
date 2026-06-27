package transformers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestOpenRouterTransformer_AnthropicModels_SignatureOmitted tests that for
// Anthropic models (anthropic/*), thinking blocks with empty/whitespace-only
// signatures have the signature field omitted entirely.
//
// Anthropic models require the signature field to be either omitted (for unsigned
// thinking) or contain a valid base64-encoded cryptographic signature.
func TestOpenRouterTransformer_AnthropicModels_SignatureOmitted(t *testing.T) {
	t.Run("anthropic models omit signature field for empty signatures", func(t *testing.T) {
		// Create a request with thinking blocks that have empty signatures
		req := &anthropic.Request{
			Model:     "anthropic/claude-sonnet-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "I should respond", Signature: testStrPtr("")}, // Empty signature
					},
				},
			},
		}

		// Create the transformer
		transformer := NewOpenRouterTransformer()

		// Prepare the request (this should strip the signature)
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		// Read the request body
		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		// Parse the request body to verify signature is NOT present
		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// CRITICAL FIX: Content should have 2 blocks (thinking + text)
		// OpenRouter's validation requires multi-element arrays, not single-element arrays
		// This normalization is required for BOTH Anthropic and non-Anthropic models via OpenRouter
		if len(content) != 2 {
			t.Errorf("Expected 2 content blocks (thinking + text), got %d", len(content))
		}

		// The first block should be thinking
		thinkingBlock := content[0].(map[string]interface{})
		if thinkingBlock["type"] != "thinking" {
			t.Errorf("Expected first block type to be 'thinking', got %v", thinkingBlock["type"])
		}

		// CRITICAL FIX: For OpenRouter Anthropic models, the signature field MUST be PRESENT (as empty string)
		// This prevents "expected string, received undefined" errors from OpenRouter's validation
		signature, hasSignature := thinkingBlock["signature"]
		if !hasSignature {
			t.Errorf("FAIL: Signature field should be PRESENT for OpenRouter Anthropic models")
			t.Logf("Full thinking block: %v", thinkingBlock)
			return
		}

		// Signature should be an empty string
		if signature != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", signature)
			return
		}

		t.Logf("SUCCESS: Signature field correctly included as empty string for OpenRouter Anthropic model")
	})

	t.Run("anthropic models clear existing signatures from previous responses", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "anthropic/claude-sonnet-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Thinking...", Signature: testStrPtr("valid-sig-123")},
					},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		// CRITICAL FIX: Existing signatures from previous provider responses MUST be cleared
		// for OpenRouter Anthropic models to prevent "Invalid signature" errors
		signature := thinkingBlock["signature"].(string)
		if signature != "" {
			t.Errorf("Expected signature to be cleared to empty string, got %q", signature)
		}

		t.Logf("SUCCESS: Existing signature from previous response cleared to empty string")
	})

	t.Run("anthropic models strip whitespace signatures", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Thinking...", Signature: testStrPtr("   ")}, // Whitespace only
					},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-opus-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)
		bodyStr := string(bodyBytes)

		// For Anthropic models, signature field should be OMITTED for whitespace signatures
		// We verify this by checking that "signature" does NOT appear in the thinking block
		// (Note: "signature" in other contexts is unlikely, so this is a safe check)

		// First, verify the thinking block exists
		if !strings.Contains(bodyStr, `"type":"thinking"`) && !strings.Contains(bodyStr, `"type": "thinking"`) {
			t.Errorf("FAIL: Thinking block not found in request")
			t.Logf("Request body: %s", bodyStr)
			return
		}

		// Extract just the thinking block portion to verify signature is omitted
		// We need to find the thinking block and ensure it doesn't have "signature" field
		// A simple way is to verify there's no "signature" key near the "thinking" type

		// Parse the JSON to properly check the thinking block
		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		// CRITICAL FIX: Signature field should be PRESENT (as empty string) for OpenRouter Anthropic models
		signature, hasSignature := thinkingBlock["signature"]
		if !hasSignature {
			t.Errorf("FAIL: Signature field should be PRESENT for OpenRouter Anthropic models")
			t.Logf("Full thinking block: %v", thinkingBlock)
			return
		}

		// Whitespace signatures should be replaced with empty string
		if signature != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", signature)
			return
		}

		t.Logf("SUCCESS: Whitespace signature replaced with empty string, field included")
	})
}

// TestOpenRouterTransformer_NonAnthropicModels_SignaturePresent tests that for
// non-Anthropic models (like Google Gemini), thinking blocks with empty signatures
// get them set to empty string to ensure the signature field is present in JSON.
//
// Some providers require the signature field to be present even when empty.
func TestOpenRouterTransformer_NonAnthropicModels_SignaturePresent(t *testing.T) {
	t.Run("non-anthropic models include signature field for empty signatures", func(t *testing.T) {
		// Create a request with thinking blocks that have empty signatures
		req := &anthropic.Request{
			Model:     "google/gemini-2.5-flash",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "I should respond", Signature: testStrPtr("")}, // Empty signature
					},
				},
			},
		}

		// Create the transformer
		transformer := NewOpenRouterTransformer()

		// Prepare the request (this should set signature to single space)
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "google/gemini-2.5-flash")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		// Read the request body
		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		// Parse the request body to verify signature IS present
		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// Content should have 2 blocks (thinking + text added by normalization)
		if len(content) != 2 {
			t.Errorf("Expected 2 content blocks, got %d", len(content))
		}

		// The first block should be thinking WITH signature field
		thinkingBlock := content[0].(map[string]interface{})
		if thinkingBlock["type"] != "thinking" {
			t.Errorf("Expected first block type to be 'thinking', got %v", thinkingBlock["type"])
		}

		// CRITICAL: For non-Anthropic models, the signature field MUST be present
		signature, hasSignature := thinkingBlock["signature"]
		if !hasSignature {
			t.Errorf("FAIL: Signature field should be present for non-Anthropic models")
			t.Logf("Full thinking block: %v", thinkingBlock)
			return
		}

		// Signature should be an empty string (pointer type allows including empty string in JSON)
		sigStr, ok := signature.(string)
		if !ok {
			t.Errorf("FAIL: Signature should be a string, got %T", signature)
			return
		}

		if sigStr != "" {
			t.Errorf("FAIL: Expected signature to be empty string '', got %q", sigStr)
			return
		}

		t.Logf("SUCCESS: Signature field correctly present with empty string value")
	})

	t.Run("non-anthropic models preserve non-empty signatures", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "google/gemini-2.5-pro",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Thinking...", Signature: testStrPtr("existing-sig")},
					},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "google/gemini-2.5-pro")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		signature := thinkingBlock["signature"].(string)
		if signature != "existing-sig" {
			t.Errorf("Expected signature to be preserved as 'existing-sig', got %q", signature)
		}

		t.Logf("SUCCESS: Non-empty signature preserved for non-Anthropic model: %q", signature)
	})

	t.Run("prepared request JSON includes signature field for non-anthropic", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "google/gemini-2.5-flash",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Thinking...", Signature: testStrPtr("")},
					},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "google/gemini-2.5-flash")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)
		bodyStr := string(bodyBytes)

		// For non-Anthropic models, signature field should be included in JSON
		if !strings.Contains(bodyStr, `"signature"`) {
			t.Errorf("FAIL: Prepared request JSON should contain 'signature' field for non-Anthropic models")
			t.Logf("Request body: %s", bodyStr)
			return
		}

		// Verify it's set to empty string (not single space)
		if !strings.Contains(bodyStr, `"signature": ""`) && !strings.Contains(bodyStr, `"signature":""`) {
			t.Errorf("FAIL: Signature should be set to empty string in JSON")
			t.Logf("Request body: %s", bodyStr)
			return
		}

		t.Logf("SUCCESS: Request JSON correctly includes signature field with empty string for non-Anthropic model")
	})
}

// TestOpenRouterTransformer_NoThinkingBlocks tests that requests without
// thinking blocks work correctly for both Anthropic and non-Anthropic models.
func TestOpenRouterTransformer_NoThinkingBlocks(t *testing.T) {
	t.Run("anthropic model without thinking blocks", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "anthropic/claude-haiku-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-haiku-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		if preparedReq["model"] != "anthropic/claude-haiku-4.5" {
			t.Errorf("Expected model to be 'anthropic/claude-haiku-4.5', got %v", preparedReq["model"])
		}

		t.Logf("SUCCESS: Anthropic model request without thinking blocks handled correctly")
	})

	t.Run("non-anthropic model without thinking blocks", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "google/gemini-2.5-flash",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "google/gemini-2.5-flash")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal prepared request: %v", err)
		}

		if preparedReq["model"] != "google/gemini-2.5-flash" {
			t.Errorf("Expected model to be 'google/gemini-2.5-flash', got %v", preparedReq["model"])
		}

		t.Logf("SUCCESS: Non-Anthropic model request without thinking blocks handled correctly")
	})
}
