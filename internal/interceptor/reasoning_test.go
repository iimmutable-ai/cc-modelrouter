// Package interceptor tests for reasoning interceptor.
package interceptor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestNewReasoningInterceptor(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config == nil {
		t.Error("expected config to be initialized")
	}
}

func TestNewReasoningInterceptorWithConfig(t *testing.T) {
	config := &ReasoningConfig{
		Enabled:               true,
		ExtractThinking:       false,
		FormatForClaudeCode:   false,
	}

	interceptor := NewReasoningInterceptorWithConfig(config)

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config != config {
		t.Error("expected config to be set")
	}
}

func TestDefaultReasoningConfig(t *testing.T) {
	config := DefaultReasoningConfig()

	if config == nil {
		t.Error("expected non-nil config")
	}
	if !config.Enabled {
		t.Error("expected enabled to be true")
	}
	if !config.ExtractThinking {
		t.Error("expected ExtractThinking to be true")
	}
	if !config.FormatForClaudeCode {
		t.Error("expected FormatForClaudeCode to be true")
	}
}

func TestReasoningInterceptor_InterceptResponse(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name          string
		response      *anthropic.Response
		expectChanges bool
	}{
		{
			name: "Response with thinking content",
			response: &anthropic.Response{
				ID:    "msg_123",
				Type:  "message",
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: "Let me think about this... This is a longer thinking process that should be formatted with proper structure for display. It exceeds 100 characters."},
					{Type: "text", Text: "Here's the answer"},
				},
			},
			expectChanges: true,
		},
		{
			name: "Response without thinking content",
			response: &anthropic.Response{
				ID:    "msg_123",
				Type:  "message",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Here's the answer"},
				},
			},
			expectChanges: false,
		},
		{
			name: "Response with empty thinking content",
			response: &anthropic.Response{
				ID:    "msg_123",
				Type:  "message",
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: ""},
					{Type: "text", Text: "Here's the answer"},
				},
			},
			expectChanges: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model: "claude-3-5-sonnet",
			}

			originalContent := make([]string, len(tt.response.Content))
			for i, block := range tt.response.Content {
				originalContent[i] = block.Thinking
			}

			err := interceptor.InterceptResponse(context.Background(), req, tt.response)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check if content was modified when expected
			if tt.expectChanges {
				changed := false
				for i, block := range tt.response.Content {
					if block.Thinking != originalContent[i] {
						changed = true
						break
					}
				}
				if !changed {
					t.Error("expected content to be changed")
				}
			}
		})
	}
}

func TestReasoningInterceptor_InterceptStreamingEvent(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name         string
		eventType    string
		data         []byte
		expectChange bool
	}{
		{
			name: "Thinking delta event",
			eventType: "content_block_delta",
			data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`),
			expectChange: true,
		},
		{
			name: "Text delta event (no change)",
			eventType: "content_block_delta",
			data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
			expectChange: false,
		},
		{
			name: "Content block start with thinking",
			eventType: "content_block_start",
			data: []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`),
			expectChange: false,
		},
		{
			name: "Invalid JSON (pass through)",
			eventType: "content_block_delta",
			data: []byte(`invalid json`),
			expectChange: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model: "claude-3-5-sonnet",
			}

			result, err := interceptor.InterceptStreamingEvent(context.Background(), req, tt.eventType, tt.data)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectChange {
				if string(result) == string(tt.data) {
					t.Error("expected data to be changed")
				}
			} else {
				if string(result) != string(tt.data) {
					t.Error("expected data to remain unchanged")
				}
			}
		})
	}
}

func TestReasoningInterceptor_formatThinkingForClaudeCode(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name     string
		thinking string
		expected string
	}{
		{
			name:     "Empty thinking",
			thinking: "",
			expected: "",
		},
		{
			name:     "Short thinking",
			thinking: "Quick thought",
			expected: "Quick thought",
		},
		{
			name:     "Long thinking with formatting",
			thinking: "This is a longer thinking process that should be formatted with proper structure for display. It exceeds 100 characters.",
			expected: "--- Thinking ---\nThis is a longer thinking process that should be formatted with proper structure for display. It exceeds 100 characters.\n--- End Thinking ---",
		},
		{
			name:     "Already formatted thinking",
			thinking: "<thinking>Already formatted</thinking>",
			expected: "<thinking>Already formatted</thinking>",
		},
		{
			name:     "Already formatted with dashes",
			thinking: "--- Thinking ---\nContent\n--- End Thinking ---",
			expected: "--- Thinking ---\nContent\n--- End Thinking ---",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.formatThinkingForClaudeCode(tt.thinking)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestReasoningInterceptor_ExtractThinkingFromResponse(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name           string
		response       *anthropic.Response
		expectedResult string
	}{
		{
			name: "Single thinking block",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: "Thinking content"},
				},
			},
			expectedResult: "Thinking content",
		},
		{
			name: "Multiple thinking blocks",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: "First thought"},
					{Type: "thinking", Thinking: "Second thought"},
				},
			},
			expectedResult: "First thought\nSecond thought",
		},
		{
			name: "Mixed content blocks",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Text content"},
					{Type: "thinking", Thinking: "Thinking content"},
					{Type: "text", Text: "More text"},
				},
			},
			expectedResult: "Thinking content",
		},
		{
			name: "No thinking blocks",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Text content"},
				},
			},
			expectedResult: "",
		},
		{
			name: "Empty thinking block",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: ""},
				},
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.ExtractThinkingFromResponse(tt.response)
			if result != tt.expectedResult {
				t.Errorf("expected %q, got %q", tt.expectedResult, result)
			}
		})
	}
}

func TestReasoningInterceptor_ExtractReasoningFromDelta(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name           string
		delta          map[string]any
		expectedResult string
	}{
		{
			name: "Thinking delta",
			delta: map[string]any{
				"type":     "thinking_delta",
				"thinking": "Thinking content",
			},
			expectedResult: "Thinking content",
		},
		{
			name: "Text delta (no thinking)",
			delta: map[string]any{
				"type": "text_delta",
				"text": "Text content",
			},
			expectedResult: "",
		},
		{
			name: "Partial JSON with thinking",
			delta: map[string]any{
				"type":         "thinking_delta",
				"partial_json": `{"thinking":"Partial thinking"}`,
			},
			expectedResult: "Partial thinking",
		},
		{
			name: "Empty delta",
			delta: map[string]any{},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.ExtractReasoningFromDelta(tt.delta)
			if result != tt.expectedResult {
				t.Errorf("expected %q, got %q", tt.expectedResult, result)
			}
		})
	}
}

func TestReasoningInterceptor_ParseReasoningEvent(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	tests := []struct {
		name           string
		eventType      string
		data           []byte
		expectedResult string
		expectError    bool
	}{
		{
			name:           "Thinking delta event",
			eventType:      "content_block_delta",
			data:           []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Thinking content"}}`),
			expectedResult: "Thinking content",
			expectError:    false,
		},
		{
			name:           "Content block event",
			eventType:      "content_block",
			data:           []byte(`{"type":"content_block","content":"Content text"}`),
			expectedResult: "Content text",
			expectError:    false,
		},
		{
			name:           "Non-reasoning event",
			eventType:      "message_stop",
			data:           []byte(`{"type":"message_stop"}`),
			expectedResult: "",
			expectError:    false,
		},
		{
			name:           "Invalid JSON",
			eventType:      "content_block_delta",
			data:           []byte(`invalid json`),
			expectedResult: "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interceptor.ParseReasoningEvent(tt.eventType, tt.data)

			if tt.expectError {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expectedResult {
					t.Errorf("expected %q, got %q", tt.expectedResult, result)
				}
			}
		})
	}
}

func TestReasoningInterceptor_Disabled(t *testing.T) {
	config := &ReasoningConfig{
		Enabled:               false,
		ExtractThinking:       true,
		FormatForClaudeCode:   true,
	}

	interceptor := NewReasoningInterceptorWithConfig(config)

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
	}

	resp := &anthropic.Response{
		Content: []anthropic.ContentBlock{
			{Type: "thinking", Thinking: "Thinking content"},
		},
	}

	// Test InterceptResponse
	err := interceptor.InterceptResponse(context.Background(), req, resp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Content should not be modified when disabled
	if resp.Content[0].Thinking != "Thinking content" {
		t.Error("expected content to remain unchanged when disabled")
	}

	// Test InterceptStreamingEvent
	data := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`)
	result, err := interceptor.InterceptStreamingEvent(context.Background(), req, "content_block_delta", data)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if string(result) != string(data) {
		t.Error("expected data to remain unchanged when disabled")
	}
}

func TestReasoningInterceptor_ExtractThinkingDisabled(t *testing.T) {
	config := &ReasoningConfig{
		Enabled:               true,
		ExtractThinking:       false,
		FormatForClaudeCode:   true,
	}

	interceptor := NewReasoningInterceptorWithConfig(config)

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
	}

	resp := &anthropic.Response{
		Content: []anthropic.ContentBlock{
			{Type: "thinking", Thinking: "Thinking content"},
		},
	}

	// Content should not be modified when extraction is disabled
	err := interceptor.InterceptResponse(context.Background(), req, resp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Content[0].Thinking != "Thinking content" {
		t.Error("expected content to remain unchanged when extraction is disabled")
	}
}

func TestReasoningInterceptor_FormatForClaudeCodeDisabled(t *testing.T) {
	config := &ReasoningConfig{
		Enabled:               true,
		ExtractThinking:       true,
		FormatForClaudeCode:   false,
	}

	interceptor := NewReasoningInterceptorWithConfig(config)

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
	}

	resp := &anthropic.Response{
		Content: []anthropic.ContentBlock{
			{Type: "thinking", Thinking: "This is a longer thinking content that would normally be formatted"},
		},
	}

	err := interceptor.InterceptResponse(context.Background(), req, resp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Content should not be formatted when formatting is disabled
	if resp.Content[0].Thinking != "This is a longer thinking content that would normally be formatted" {
		t.Errorf("expected content to remain unchanged, got %q", resp.Content[0].Thinking)
	}
}

func TestReasoningInterceptor_StreamingWithComplexDelta(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
	}

	// Test with a complex delta containing partial JSON
	data := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Step 1: Analyze the problem. Step 2: Consider options. Step 3: Decide. This is a longer thinking process that should be formatted with proper structure for display. It exceeds 100 characters."}}`)
	result, err := interceptor.InterceptStreamingEvent(context.Background(), req, "content_block_delta", data)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Parse result to verify it's valid JSON
	var resultData map[string]any
	if err := json.Unmarshal(result, &resultData); err != nil {
		t.Errorf("result is not valid JSON: %v", err)
	}

	// Verify the thinking was formatted
	if delta, ok := resultData["delta"].(map[string]any); ok {
		if thinking, ok := delta["thinking"].(string); ok {
			expected := "--- Thinking ---\nStep 1: Analyze the problem. Step 2: Consider options. Step 3: Decide. This is a longer thinking process that should be formatted with proper structure for display. It exceeds 100 characters.\n--- End Thinking ---"
			if thinking != expected {
				t.Errorf("expected formatted thinking, got %q", thinking)
			}
		}
	}
}

func TestReasoningInterceptor_ExtractThinkingMultipleBlocks(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	resp := &anthropic.Response{
		Content: []anthropic.ContentBlock{
			{Type: "thinking", Thinking: "First"},
			{Type: "thinking", Thinking: "Second"},
			{Type: "thinking", Thinking: "Third"},
		},
	}

	result := interceptor.ExtractThinkingFromResponse(resp)
	expected := "First\nSecond\nThird"

	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestReasoningInterceptor_VerifyResponseNotModifiedWhenNoThinking(t *testing.T) {
	interceptor := NewReasoningInterceptor()

	originalContent := []anthropic.ContentBlock{
		{Type: "text", Text: "Some text content"},
		{Type: "tool_use", Name: "search", Input: json.RawMessage(`{"query":"test"}`)},
	}

	resp := &anthropic.Response{
		ID:      "msg_123",
		Type:    "message",
		Content: originalContent,
	}

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
	}

	err := interceptor.InterceptResponse(context.Background(), req, resp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify content wasn't modified
	if len(resp.Content) != len(originalContent) {
		t.Error("expected content length to remain unchanged")
	}

	for i, block := range resp.Content {
		if block.Type != originalContent[i].Type {
			t.Errorf("expected block type %q, got %q", originalContent[i].Type, block.Type)
		}
		if block.Thinking != originalContent[i].Thinking {
			t.Errorf("expected block content %q, got %q", originalContent[i].Thinking, block.Thinking)
		}
	}
}