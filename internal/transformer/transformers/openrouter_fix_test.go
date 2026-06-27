package transformers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestOpenRouterFix_InvalidSignatureError tests the fix for the "Invalid signature in
// thinking block" error that occurred when failing over to OpenRouter with Anthropic
// models.
//
// The error was: "Invalid input: expected string, received undefined" for the signature field.
//
// Root cause: OpenRouter's Anthropic models require the signature field to be PRESENT
// as a string value, even when empty. The old code omitted the field, causing OpenRouter
// to see it as "undefined" and reject the request.
//
// Fix: Set signature to empty string pointer (&"") which marshals to "signature": ""
// This ensures the field is PRESENT in the JSON, satisfying OpenRouter's validation.
func TestOpenRouterFix_InvalidSignatureError(t *testing.T) {
	t.Run("anthropic model with empty signature includes field", func(t *testing.T) {
		// Simulate a request after failover - thinking block with empty signature
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
						{Type: "thinking", Thinking: "Extended thinking here...", Signature: testStrPtr("")},
					},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		// Read request body
		bodyBytes := make([]byte, httpReq.ContentLength)
		n, _ := httpReq.Body.Read(bodyBytes)
		bodyStr := string(bodyBytes[:n])

		// CRITICAL FIX: The signature field should be PRESENT for OpenRouter Anthropic models
		// We verify by checking the JSON structure directly
		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// CRITICAL FIX: Should have 2 blocks (thinking + text)
		// OpenRouter's validation requires multi-element arrays, not single-element arrays
		// This normalization is required for BOTH Anthropic and non-Anthropic models via OpenRouter
		if len(content) != 2 {
			t.Errorf("Expected 2 content blocks (thinking + text), got %d", len(content))
		}

		// The first block should be thinking
		thinkingBlock := content[0].(map[string]interface{})

		// CRITICAL FIX: Signature field should be PRESENT for OpenRouter Anthropic models
		// This prevents "expected string, received undefined" errors
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

		// Verify the raw JSON contains "signature": "" (empty string)
		if !strings.Contains(bodyStr, `"signature":"`) {
			t.Errorf("FAIL: Signature field not found in JSON")
			t.Logf("Request body: %s", bodyStr)
			return
		}

		t.Logf("PASS: Signature field correctly included as empty string for OpenRouter Anthropic model")
		t.Logf("   Request will pass OpenRouter's validation")
	})

	t.Run("anthropic model with whitespace signature includes empty field", func(t *testing.T) {
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

		// Read the request body
		bodyBytes := make([]byte, httpReq.ContentLength)
		n, _ := httpReq.Body.Read(bodyBytes)
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		// CRITICAL FIX: Signature field should be PRESENT (with empty string) for OpenRouter
		signature, hasSignature := thinkingBlock["signature"]
		if !hasSignature {
			t.Errorf("FAIL: Signature field should be PRESENT for OpenRouter Anthropic models")
			return
		}

		// Whitespace signatures are replaced with empty string
		if signature != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", signature)
			return
		}

		t.Logf("PASS: Whitespace signature replaced with empty string, field included")
	})

	t.Run("anthropic model clears signatures from previous responses", func(t *testing.T) {
		// Test that signatures from previous provider responses are cleared
		// This prevents "Invalid signature" errors when failing over from GLM to OpenRouter-Anthropic
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
						{Type: "thinking", Thinking: "Thinking...", Signature: testStrPtr("abcd1234efgh5678")},
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

		var preparedReq map[string]interface{}
		json.Unmarshal(bodyBytes, &preparedReq)

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		// CRITICAL FIX: Signatures from previous provider responses MUST be cleared
		// for OpenRouter Anthropic models to prevent "Invalid signature" errors
		signature := thinkingBlock["signature"].(string)
		if signature != "" {
			t.Errorf("FAIL: Signature from previous response should be cleared to empty string, got %q", signature)
			return
		}

		t.Logf("PASS: Signature from previous provider response correctly cleared")
	})

	t.Run("verify request structure for OpenRouter compatibility", func(t *testing.T) {
		// Create a more complex scenario with multiple messages
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Initial request"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "First thinking...", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Follow-up question"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Second thinking...", Signature: testStrPtr("   ")}, // Whitespace
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
		n, _ := httpReq.Body.Read(bodyBytes)
		bodyStr := string(bodyBytes[:n])

		// Parse the JSON to verify signature fields are included in thinking blocks
		var preparedReq map[string]interface{}
		json.Unmarshal(bodyBytes, &preparedReq)

		messages := preparedReq["messages"].([]interface{})

		// Check both assistant messages with thinking blocks
		// Message 1 (index 1): First assistant message with thinking
		msg1 := messages[1].(map[string]interface{})
		content1 := msg1["content"].([]interface{})
		thinking1 := content1[0].(map[string]interface{})

		// CRITICAL FIX: First thinking block should have signature INCLUDED (as empty string)
		sig1, hasSig1 := thinking1["signature"]
		if !hasSig1 {
			t.Errorf("FAIL: First thinking block should have signature field included")
			return
		}
		if sig1 != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", sig1)
			return
		}

		// Message 3 (index 3): Second assistant message with thinking
		msg3 := messages[3].(map[string]interface{})
		content3 := msg3["content"].([]interface{})
		thinking3 := content3[0].(map[string]interface{})

		// CRITICAL FIX: Second thinking block should have signature INCLUDED (as empty string)
		sig3, hasSig3 := thinking3["signature"]
		if !hasSig3 {
			t.Errorf("FAIL: Second thinking block should have signature field included")
			return
		}
		if sig3 != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", sig3)
			return
		}

		// Verify no placeholder signatures remain in the entire request
		if strings.Contains(bodyStr, `(signature)`) {
			t.Errorf("FAIL: Found invalid placeholder signature in request")
			return
		}

		// Verify the raw JSON contains signature fields
		if !strings.Contains(bodyStr, `"signature":"`) {
			t.Errorf("FAIL: Signature field not found in JSON")
			return
		}

		t.Logf("PASS: All thinking blocks have signature field included for OpenRouter Anthropic models")
		t.Logf("   Request structure is compatible with OpenRouter's validation")
	})
}
