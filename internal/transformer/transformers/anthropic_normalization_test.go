package transformers

import (
	"encoding/json"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestNormalizeSingleElementContent_AllRoles tests that content normalization
// processes ALL message roles, not just assistant messages.
//
// This is a regression test for the bug where user messages with single
// thinking blocks were not normalized, causing OpenRouter to reject them
// with "expected string, received array" errors.
func TestNormalizeSingleElementContent_AllRoles(t *testing.T) {
	tests := []struct {
		name           string
		messages       []anthropic.Message
		wantContentLen int // expected content length after normalization
		wantHasText    bool
	}{
		{
			name: "user message with single thinking block should be normalized",
			messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Previous thinking"}},
				},
			},
			wantContentLen: 2, // thinking + text(" ")
			wantHasText:    true,
		},
		{
			name: "user message with single image block should be normalized",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{{
						Type: "image",
						Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: "abc123"},
					}},
				},
			},
			wantContentLen: 2, // image + text(" ")
			wantHasText:    true,
		},
		{
			name: "assistant message with single thinking block should be normalized",
			messages: []anthropic.Message{
				{
					Role:    "assistant",
					Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Deep thought"}},
				},
			},
			wantContentLen: 2, // thinking + text(" ")
			wantHasText:    true,
		},
		{
			name: "user message with text block should NOT add extra text",
			messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
			},
			wantContentLen: 1,
			wantHasText:    true,
		},
		{
			name: "user message with thinking AND text should NOT add extra text",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "Thinking"},
						{Type: "text", Text: "Hello"},
					},
				},
			},
			wantContentLen: 2,
			wantHasText:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "test-model",
				MaxTokens: 1024,
				Messages:  tt.messages,
			}

			// Apply normalization
			normalizeSingleElementContent(req)

			// Verify the message was normalized
			if len(req.Messages) != 1 {
				t.Fatalf("Expected 1 message, got %d", len(req.Messages))
			}

			msg := req.Messages[0]
			if len(msg.Content) != tt.wantContentLen {
				t.Errorf("Expected content length %d, got %d", tt.wantContentLen, len(msg.Content))
			}

			// Check if there's a text block
			hasText := false
			for _, block := range msg.Content {
				if block.Type == "text" {
					hasText = true
					break
				}
			}
			if hasText != tt.wantHasText {
				t.Errorf("Expected hasText=%v, got %v", tt.wantHasText, hasText)
			}
		})
	}
}

// TestNormalizeSingleElementContent_JSONOutput tests that the normalized content
// produces valid JSON that would pass OpenRouter validation.
//
// The key issue: single non-text blocks serialize as arrays, but OpenRouter
// expects either a string or a multi-element array.
func TestNormalizeSingleElementContent_JSONOutput(t *testing.T) {
	tests := []struct {
		name     string
		messages []anthropic.Message
	}{
		{
			name: "user message with single thinking becomes multi-element array",
			messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "thinking", Thinking: "test"}},
				},
			},
		},
		{
			name: "user message with single image becomes multi-element array",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{{
						Type:   "image",
						Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"},
					}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "test-model",
				MaxTokens: 1024,
				Messages:  tt.messages,
			}

			// Apply normalization
			normalizeSingleElementContent(req)

			// Marshal and check output
			jsonBytes, err := json.Marshal(req.Messages[0].Content)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Verify it's a multi-element array (not single element)
			var content []json.RawMessage
			if err := json.Unmarshal(jsonBytes, &content); err != nil {
				t.Fatalf("Failed to unmarshal as array: %v", err)
			}

			// Should be 2 elements: [thinking/image, text(" ")]
			if len(content) != 2 {
				t.Errorf("Expected 2 content blocks, got %d. JSON: %s", len(content), string(jsonBytes))
			}

			// First block should be thinking/image, second should be text
			var blocks []map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &blocks); err != nil {
				t.Fatalf("Failed to unmarshal as object array: %v", err)
			}

			if len(blocks) == 2 {
				// Verify first block type matches what we put in
				firstType := blocks[0]["type"].(string)
				secondType := blocks[1]["type"].(string)
				if secondType != "text" {
					t.Errorf("Expected second block to be text, got: %s", secondType)
				}
				t.Logf("Normalized content: first block type=%s, second block type=%s", firstType, secondType)
			}
		})
	}
}

// TestValidateAndRepairBlocks_AllBlockTypes tests that validation catches
// blocks with missing required fields for ALL block types.
func TestValidateAndRepairBlocks_AllBlockTypes(t *testing.T) {
	tests := []struct {
		name          string
		messages      []anthropic.Message
		wantBlockType string // what the block should become after repair
	}{
		{
			name: "image with nil Source gets converted to text",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{{
						Type:   "image",
						Source: nil, // nil source causes [N, "data"] error
					}},
				},
			},
			wantBlockType: "text",
		},
		{
			name: "image with empty Data gets converted to text",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{{
						Type:   "image",
						Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: ""},
					}},
				},
			},
			wantBlockType: "text",
		},
		{
			name: "document with nil DocumentSource gets converted to text",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{{
						Type:           "document",
						DocumentSource: nil, // nil source causes validation error
					}},
				},
			},
			wantBlockType: "text",
		},
		{
			name: "thinking with empty content gets converted to text",
			messages: []anthropic.Message{
				{
					Role: "assistant",
					Content: anthropic.MessageContent{{
						Type:     "thinking",
						Thinking: "", // empty thinking
					}},
				},
			},
			wantBlockType: "text",
		},
		{
			name: "valid text block remains unchanged",
			messages: []anthropic.Message{
				{
					Role:    "user",
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
			},
			wantBlockType: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "test-model",
				MaxTokens: 1024,
				Messages:  tt.messages,
			}

			// Apply validation and repair
			validateAndRepairBlocks(req)

			// Check the result
			if len(req.Messages) != 1 {
				t.Fatalf("Expected 1 message, got %d", len(req.Messages))
			}

			msg := req.Messages[0]
			if len(msg.Content) != 1 {
				t.Errorf("Expected 1 content block after repair, got %d", len(msg.Content))
			}

			if msg.Content[0].Type != tt.wantBlockType {
				t.Errorf("Expected block type %s, got %s", tt.wantBlockType, msg.Content[0].Type)
			}
		})
	}
}