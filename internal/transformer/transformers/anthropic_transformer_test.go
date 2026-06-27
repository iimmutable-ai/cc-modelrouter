package transformers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// Test helper function for creating string pointers in tests
func testStrPtr(s string) *string {
	return &s
}

// TestAnthropicTransformer_SignatureNormalization tests that thinking blocks
// with empty/whitespace-only signatures have them stripped (omitted from JSON).
//
// Anthropic's API rejects whitespace-only signatures, so we strip them to allow
// MarshalJSON to omit the signature field entirely. This is different from GLM
// transformers which require the signature field to be present.
//
// This is a regression test for the bug where failover from GLM to Anthropic
// returns 400 Bad Request with error: "Invalid 'signature' in 'thinking' block".
func TestAnthropicTransformer_SignatureNormalization(t *testing.T) {
	t.Run("thinking blocks with empty signature get stripped", func(t *testing.T) {
		// Create a request with thinking blocks that have empty signatures
		req := &anthropic.Request{
			Model:     "claude-opus-4.5",
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
		transformer := NewAnthropicTransformer()

		// Prepare the request (this should strip signature)
		httpReq, err := transformer.PrepareRequest(req, "https://api.anthropic.com", "test-key", "claude-opus-4.5")
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

		// Content should have 1 block (thinking only)
		// Anthropic API accepts single thinking blocks without normalization
		if len(content) != 1 {
			t.Errorf("Expected 1 content block, got %d", len(content))
		}

		// The block should be thinking WITHOUT signature field
		thinkingBlock := content[0].(map[string]interface{})
		if thinkingBlock["type"] != "thinking" {
			t.Errorf("Expected first block type to be 'thinking', got %v", thinkingBlock["type"])
		}

		// CRITICAL: The signature field should NOT be present for Anthropic
		// Empty signatures are stripped so MarshalJSON omits them
		if _, hasSignature := thinkingBlock["signature"]; hasSignature {
			t.Errorf("FAIL: Signature field should NOT be present in thinking block for Anthropic (it will reject whitespace signatures)")
			t.Logf("Full thinking block: %v", thinkingBlock)
			return
		}

		t.Logf("SUCCESS: Signature field is correctly omitted from thinking block")
	})

	t.Run("multiple thinking blocks all get signatures stripped", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Use tools"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "First thought", Signature: testStrPtr("")},
						{Type: "tool_use", ID: "toolu_123", Name: "test", Input: json.RawMessage(`{}`)},
					},
				},
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "tool_result", ID: "toolu_123", Content: anthropic.MessageContent{{Type: "text", Text: "Result"}}},
					},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Second thought", Signature: testStrPtr("")},
					},
				},
			},
		}

		transformer := NewAnthropicTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://api.anthropic.com", "test-key", "claude-opus-4.5")
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

		// Check first assistant message (index 1)
		assistantMsg1 := messages[1].(map[string]interface{})
		content1 := assistantMsg1["content"].([]interface{})

		// Should have thinking + tool_use + text (from normalization)
		foundThinking1 := false
		for _, block := range content1 {
			blockMap := block.(map[string]interface{})
			if blockMap["type"] == "thinking" {
				foundThinking1 = true
				// For Anthropic, empty signatures should be stripped (not present in JSON)
				if _, hasSig := blockMap["signature"]; hasSig {
					t.Errorf("FAIL: First assistant message thinking block should NOT have signature field")
				}
			}
		}
		if !foundThinking1 {
			t.Error("FAIL: No thinking block found in first assistant message")
		}

		// Check second assistant message (index 3)
		assistantMsg2 := messages[3].(map[string]interface{})
		content2 := assistantMsg2["content"].([]interface{})

		// Should have thinking + text (from normalization)
		if len(content2) < 1 {
			t.Error("FAIL: Second assistant message has no content")
			return
		}

		thinkingBlock2 := content2[0].(map[string]interface{})
		if thinkingBlock2["type"] != "thinking" {
			t.Errorf("Expected first block to be thinking, got %v", thinkingBlock2["type"])
		}

		if _, hasSig := thinkingBlock2["signature"]; hasSig {
			t.Errorf("FAIL: Second assistant message thinking block should NOT have signature field")
		}

		t.Logf("SUCCESS: All thinking blocks correctly omit signature field for Anthropic")
	})

	t.Run("prepared request JSON omits empty signature field", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "claude-opus-4.5",
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

		transformer := NewAnthropicTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://api.anthropic.com", "test-key", "claude-opus-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		bodyBytes := make([]byte, httpReq.ContentLength)
		httpReq.Body.Read(bodyBytes)
		bodyStr := string(bodyBytes)

		// For Anthropic, empty signatures should be omitted from JSON
		if strings.Contains(bodyStr, `"signature"`) {
			t.Errorf("FAIL: Prepared request JSON should NOT contain 'signature' field for empty signatures")
			t.Logf("Request body: %s", bodyStr)
			return
		}

		t.Logf("SUCCESS: Request JSON correctly omits signature field for empty signatures")
	})
}

// TestAnthropicTransformer_SignatureAlreadySet tests that if signature
// is already set (non-empty), it is preserved.
func TestAnthropicTransformer_SignatureAlreadySet(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-opus-4.5",
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

	transformer := NewAnthropicTransformer()
	httpReq, err := transformer.PrepareRequest(req, "https://api.anthropic.com", "test-key", "claude-opus-4.5")
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

	t.Logf("SUCCESS: Existing signature preserved: %q", signature)
}
