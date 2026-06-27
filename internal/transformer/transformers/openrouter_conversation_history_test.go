package transformers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestOpenRouter_ConversationHistoryWithThinkingBlocks tests the critical fix for
// "Invalid input: expected string, received array" errors that occur when the
// conversation history contains assistant messages with thinking blocks from previous
// GLM responses.
//
// This is NOT a failover issue - it occurs on direct requests (Target 0) when:
// 1. GLM has already returned assistant messages with thinking blocks
// 2. These are in the conversation history
// 3. A new request goes to OpenRouter Anthropic model
//
// Root cause: OpenRouter transformer incorrectly assumes Anthropic models accept
// single thinking blocks without normalization. OpenRouter's validation is stricter.
//
// Fix: Always normalize thinking blocks for OpenRouter, regardless of model type.
func TestOpenRouter_ConversationHistoryWithThinkingBlocks(t *testing.T) {
	t.Run("anthropic model with GLM thinking block in history - FAILS before fix", func(t *testing.T) {
		// Simulate conversation history after GLM has responded
		// This is what causes the error in production
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "First question"}},
				},
				{
					Role: "assistant",
					// GLM returns assistant messages with single thinking blocks
					// These get added to conversation history as-is
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "GLM extended thinking response", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Follow-up question with thinking"}},
				},
			},
		}

		transformer := NewOpenRouterTransformer()
		httpReq, err := transformer.PrepareRequest(req, "https://openrouter.ai/api", "test-key", "anthropic/claude-opus-4.5")
		if err != nil {
			t.Fatalf("PrepareRequest failed: %v", err)
		}

		// Read request body
		bodyBytes := make([]byte, httpReq.ContentLength)
		n, _ := httpReq.Body.Read(bodyBytes)
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})

		// Check the assistant message (index 1) from conversation history
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// CRITICAL FIX: After the fix, content should have 2 blocks (thinking + text)
		// This is required by OpenRouter's validation
		// Before the fix: len(content) == 1 (FAILS OpenRouter validation)
		// After the fix: len(content) == 2 (PASSES OpenRouter validation)
		if len(content) != 2 {
			t.Errorf("FAIL: Expected 2 content blocks after normalization (thinking + text), got %d", len(content))
			t.Errorf("This indicates the fix has NOT been applied yet")
			t.Logf("OpenRouter will reject single-element arrays with 'expected string, received array'")
			return
		}

		// Verify first block is thinking
		thinkingBlock := content[0].(map[string]interface{})
		if thinkingBlock["type"] != "thinking" {
			t.Errorf("Expected first block to be thinking, got %v", thinkingBlock["type"])
		}

		// Verify second block is text with placeholder
		textBlock := content[1].(map[string]interface{})
		if textBlock["type"] != "text" {
			t.Errorf("Expected second block to be text, got %v", textBlock["type"])
		}
		if textBlock["text"] != "[thinking context removed for provider compatibility]" {
			t.Errorf("Expected text block to contain placeholder, got %q", textBlock["text"])
		}

		t.Logf("PASS: Thinking block normalized to multi-element array")
		t.Logf("   OpenRouter will accept this format")
	})

	t.Run("anthropic model with multiple thinking blocks in history", func(t *testing.T) {
		// Simulate multiple GLM responses in conversation history
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Question 1"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "GLM thinking 1", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Question 2"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "GLM thinking 2", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Question 3 with thinking"}},
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
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})

		// Check first assistant message (index 1)
		msg1 := messages[1].(map[string]interface{})
		content1 := msg1["content"].([]interface{})
		if len(content1) != 2 {
			t.Errorf("FAIL: First assistant message should have 2 blocks after normalization, got %d", len(content1))
			return
		}

		// Check second assistant message (index 3)
		msg3 := messages[3].(map[string]interface{})
		content3 := msg3["content"].([]interface{})
		if len(content3) != 2 {
			t.Errorf("FAIL: Second assistant message should have 2 blocks after normalization, got %d", len(content3))
			return
		}

		t.Logf("PASS: All thinking blocks in conversation history normalized")
		t.Logf("   Multiple assistant messages with thinking blocks properly handled")
	})

	t.Run("direct request with thinking block - should normalize", func(t *testing.T) {
		// Even direct requests (not from conversation history) with thinking blocks
		// need normalization for OpenRouter
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Question with thinking"}},
				},
				{
					Role: "assistant",
					// This could be from a previous assistant response included in context
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Previous thinking", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Continue"}},
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
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// CRITICAL FIX: Direct requests also need normalization for OpenRouter
		if len(content) != 2 {
			t.Errorf("FAIL: Expected 2 content blocks (thinking + text), got %d", len(content))
			t.Errorf("OpenRouter requires multi-element arrays, not single-element arrays")
			return
		}

		t.Logf("PASS: Direct request with thinking block properly normalized")
	})

	t.Run("non-anthropic model with thinking block - already normalized", func(t *testing.T) {
		// Non-Anthropic models already get normalized
		// This test verifies the behavior is maintained
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
		n, _ := httpReq.Body.Read(bodyBytes)
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})

		// Non-Anthropic models should also have 2 blocks after normalization
		if len(content) != 2 {
			t.Errorf("Expected 2 content blocks for non-Anthropic model, got %d", len(content))
			return
		}

		t.Logf("PASS: Non-Anthropic model with thinking block properly normalized")
	})

	t.Run("signature field properly set for normalized blocks", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Question"}},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "GLM thinking", Signature: testStrPtr("")},
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
		bodyBytes = bodyBytes[:n]
		bodyStr := string(bodyBytes)

		// Verify signature field is present in JSON
		if !strings.Contains(bodyStr, `"signature"`) {
			t.Errorf("FAIL: Signature field should be present in JSON")
			return
		}

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		assistantMsg := messages[1].(map[string]interface{})
		content := assistantMsg["content"].([]interface{})
		thinkingBlock := content[0].(map[string]interface{})

		// Signature should be present and empty
		signature, hasSignature := thinkingBlock["signature"]
		if !hasSignature {
			t.Errorf("FAIL: Signature field should be present for OpenRouter Anthropic models")
			return
		}
		if signature != "" {
			t.Errorf("FAIL: Expected empty string signature, got %v", signature)
			return
		}

		t.Logf("PASS: Signature field properly set to empty string")
	})
}

// TestOpenRouter_UserThinkingBlocks tests that user messages with thinking blocks
// are converted to text (not normalized). This is important because Claude Code
// may resend previous assistant responses as user messages.
func TestOpenRouter_UserThinkingBlocks(t *testing.T) {
	t.Run("user message with thinking block converted to text", func(t *testing.T) {
		req := &anthropic.Request{
			Model:     "anthropic/claude-opus-4.5",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    "user",
					// Claude Code sometimes resends assistant responses as user messages
					// These thinking blocks should be converted to text with <thinking> tags
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Previous assistant thinking", Signature: testStrPtr("")},
					},
				},
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "New question"}},
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
		bodyBytes = bodyBytes[:n]

		var preparedReq map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &preparedReq); err != nil {
			t.Fatalf("Failed to unmarshal request: %v", err)
		}

		messages := preparedReq["messages"].([]interface{})
		userMsg := messages[0].(map[string]interface{})

		// User message content should be a string (not array)
		// because convertUserThinkingToText converts thinking to text
		content, isString := userMsg["content"].(string)
		if !isString {
			t.Errorf("FAIL: User message content should be string after thinking conversion, got %T", userMsg["content"])
			return
		}

		// Should contain <thinking> tags
		if !strings.Contains(content, "<thinking>") {
			t.Errorf("FAIL: User message content should contain <thinking> tags, got %q", content)
			return
		}

		t.Logf("PASS: User thinking block converted to text with <thinking> tags")
	})
}
