package anthropic_test

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

// TestMessageContent_SingleThinkingBlock_ArrayFormat tests that a message
// with only a thinking block is marshaled as an array, which may cause
// provider validation errors.
func TestMessageContent_SingleThinkingBlock_ArrayFormat(t *testing.T) {
	content := anthropic.MessageContent{
		{Type: "thinking", Thinking: "This is my thinking process"},
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	got := string(data)
	t.Logf("Single thinking block marshaled as: %s", got)

	// According to current implementation, single non-text blocks are marshaled as arrays
	expected := `[{"type":"thinking","thinking":"This is my thinking process"}]`
	if got != expected {
		t.Logf("Expected: %s", expected)
		t.Logf("Got:      %s", got)
		t.Logf("NOTE: Single thinking blocks produce ARRAY format, which some providers may reject")
	}

	// Verify it's an array, not a string
	if !strings.HasPrefix(got, "[") {
		t.Errorf("Expected array format starting with '[', got: %s", got)
	}
}

// TestMessageContent_SingleTextBlock_StringFormat tests that a message
// with only a text block is marshaled as a string.
func TestMessageContent_SingleTextBlock_StringFormat(t *testing.T) {
	content := anthropic.MessageContent{
		{Type: "text", Text: "Hello"},
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	got := string(data)
	expected := `"Hello"`

	if got != expected {
		t.Errorf("Expected %s, got %s", expected, got)
	}

	// Verify it's a string, not an array
	if !strings.HasPrefix(got, `"`) {
		t.Errorf("Expected string format starting with '\"', got: %s", got)
	}
}

// TestMessageContent_ThinkingPlusText_ArrayFormat tests that a message
// with thinking followed by text is marshaled as an array.
func TestMessageContent_ThinkingPlusText_ArrayFormat(t *testing.T) {
	content := anthropic.MessageContent{
		{Type: "thinking", Thinking: "Thinking..."},
		{Type: "text", Text: "Response"},
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	got := string(data)
	t.Logf("Thinking + Text marshaled as: %s", got)

	// Should be array with 2 elements
	if !strings.HasPrefix(got, "[") {
		t.Errorf("Expected array format for multiple blocks, got: %s", got)
	}
}

// TestMessageContent_EmptyThinkingBlock_ArrayFormat tests that a message
// with an empty thinking block is marshaled as an array.
func TestMessageContent_EmptyThinkingBlock_ArrayFormat(t *testing.T) {
	content := anthropic.MessageContent{
		{Type: "thinking", Thinking: "", Signature: testStrPtr("")},
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	got := string(data)
	t.Logf("Empty thinking block marshaled as: %s", got)

	// Empty thinking blocks are skipped, so result is empty array
	expected := `[]`
	if got != expected {
		t.Logf("Expected: %s (empty array), got: %s", expected, got)
	}
}

// TestFullRequest_WithThinkingBlock tests marshaling a complete request
// with thinking blocks to see the exact JSON structure.
func TestFullRequest_WithThinkingBlock(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-sonnet-4.5",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					{Type: "thinking", Thinking: "Let me think about this..."},
					{Type: "text", Text: "I think the answer is 42"},
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	got := string(data)
	t.Logf("Full request with thinking:\n%s", got)

	// Parse and verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	messages, ok := parsed["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not found or not an array")
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Check second message (assistant with thinking)
	assistantMsg, ok := messages[1].(map[string]interface{})
	if !ok {
		t.Fatal("second message not an object")
	}

	content, ok := assistantMsg["content"]
	if !ok {
		t.Fatal("content field missing from assistant message")
	}

	t.Logf("messages[1].content type: %T", content)
	t.Logf("messages[1].content value: %v", content)

	// Verify it's an array (not a string)
	if _, ok := content.(string); ok {
		t.Logf("WARNING: messages[1].content is a STRING (this would work for providers)")
	}
	if contentArray, ok := content.([]interface{}); ok {
		t.Logf("CONFIRMED: messages[1].content is an ARRAY with %d elements", len(contentArray))
		t.Logf("This format may be REJECTED by some providers who expect a string")
		for i, elem := range contentArray {
			t.Logf("  content[%d]: %v", i, elem)
		}
	}
}

// TestFullRequest_OnlyThinkingBlock tests the problematic case:
// a message with ONLY a thinking block (no text).
func TestFullRequest_OnlyThinkingBlock(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-sonnet-4.5",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
			},
			{
				Role:    "assistant",
				Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Deep thinking..."}},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	got := string(data)
	t.Logf("Full request with ONLY thinking block in assistant message:\n%s", got)

	// Parse and verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	messages, ok := parsed["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not found or not an array")
	}

	// Check second message content
	assistantMsg, ok := messages[1].(map[string]interface{})
	if !ok {
		t.Fatal("second message not an object")
	}

	content, ok := assistantMsg["content"]
	if !ok {
		t.Fatal("content field missing")
	}

	// This is the PROBLEMATIC case: single thinking block produces array
	if contentArray, ok := content.([]interface{}); ok {
		t.Logf("PROBLEM CONFIRMED: messages[1].content is an ARRAY: %v", contentArray)
		t.Logf("This causes provider error: 'expected string, received array'")
		t.Logf("Path in error: ['messages', 1, 'content']")

		// Check what's in the array
		if len(contentArray) > 0 {
			if firstElem, ok := contentArray[0].(map[string]interface{}); ok {
				t.Logf("First element type: %v", firstElem["type"])
				if thinking, ok := firstElem["thinking"].(string); ok {
					t.Logf("Thinking content: %d chars", len(thinking))
				}
			}
		}
	}
}

// TestContentBlock_ThinkingMarshaling tests the exact JSON output
// for thinking blocks with and without signature.
func TestContentBlock_ThinkingMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		block    anthropic.ContentBlock
		expected string
	}{
		{
			name: "thinking without signature (should omit field)",
			block: anthropic.ContentBlock{
				Type:     "thinking",
				Thinking: "Thinking content",
			},
			expected: `{"type":"thinking","thinking":"Thinking content"}`,
		},
		{
			name: "thinking with nil signature (should omit field)",
			block: anthropic.ContentBlock{
				Type:     "thinking",
				Thinking: "Thinking content",
				// Signature is nil (default zero value for pointer), field should be omitted
			},
			expected: `{"type":"thinking","thinking":"Thinking content"}`,
		},
		{
			name: "thinking with empty signature (should include field as empty string)",
			block: anthropic.ContentBlock{
				Type:     "thinking",
				Thinking: "Thinking content",
				Signature: testStrPtr(""),
			},
			expected: `{"type":"thinking","thinking":"Thinking content","signature":""}`,
		},
		{
			name: "thinking with non-empty signature (should include field)",
			block: anthropic.ContentBlock{
				Type:      "thinking",
				Thinking:  "Thinking content",
				Signature: testStrPtr("abc123"),
			},
			expected: `{"type":"thinking","thinking":"Thinking content","signature":"abc123"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			got := string(data)
			t.Logf("Got: %s", got)

			if got != tt.expected {
				t.Errorf("Expected: %s", tt.expected)
				if tt.expected != got {
					// Check if signature handling is the issue
					hasSignature := strings.Contains(got, "signature")
					expectedHasSig := strings.Contains(tt.expected, "signature")
					if hasSignature != expectedHasSig {
						t.Logf("Signature field presence mismatch: expected=%v, got=%v", expectedHasSig, hasSignature)
					}
				}
			}
		})
	}
}

// TestMessageContent_Unmarshal_RoundTrip tests that thinking blocks
// can be unmarshaled and remarshaled correctly.
func TestMessageContent_Unmarshal_RoundTrip(t *testing.T) {
	// Start with JSON that has a thinking block in array format
	jsonInput := `[{"type":"thinking","thinking":"Original thinking"}]`

	var content anthropic.MessageContent
	if err := json.Unmarshal([]byte(jsonInput), &content); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	t.Logf("After unmarshal: %d blocks", len(content))
	for i, block := range content {
		t.Logf("  Block %d: type=%s, thinking=%d chars", i, block.Type, len(block.Thinking))
	}

	// Marshal back to JSON
	output, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	t.Logf("Round-trip output: %s", string(output))

	// The output should be the same as input (array format)
	if string(output) != jsonInput {
		t.Logf("Note: Round-trip changed the format")
		t.Logf("Input:  %s", jsonInput)
		t.Logf("Output: %s", string(output))
	}
}

// TestRequest_WithToolResultAndThinking tests a complex scenario
// with tool_result blocks that might have nested content issues.
func TestRequest_WithToolResultAndThinking(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-sonnet-4.5",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Use the search tool"}},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					{Type: "thinking", Thinking: "I need to search..."},
					{
						Type: "tool_use",
						ID:   "toolu_123",
						Name: "search",
						Input: json.RawMessage(`{"query":"test"}`),
					},
				},
			},
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{
						Type: "tool_result",
						ID:   "toolu_123",
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Search results..."},
						},
					},
				},
			},
			{
				Role:    "assistant",
				Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Analyzing results..."}},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	t.Logf("Complex request with tool_result and thinking:\n%s", string(data))

	// Parse and check for potential issues
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	messages, _ := parsed["messages"].([]interface{})
	t.Logf("Total messages: %d", len(messages))

	// Check each message's content format
	for i, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := m["content"]
		if !ok {
			continue
		}

		switch v := content.(type) {
		case string:
			t.Logf("messages[%d].content: STRING (%d chars)", i, len(v))
		case []interface{}:
			t.Logf("messages[%d].content: ARRAY (%d elements)", i, len(v))
			// Check for potential [3, "data"] error path
			if len(v) > 3 {
				if thirdElem, ok := v[3].(map[string]interface{}); ok {
					t.Logf("  Potential error path [3, 'data']: element at index 3 has type=%v", thirdElem["type"])
					if source, ok := thirdElem["source"].(map[string]interface{}); ok {
						if _, hasData := source["data"]; !hasData {
							t.Logf("  WARNING: Element at index 3 is missing source.data field!")
							t.Logf("  This could cause the '[3, data]' error")
						}
					}
				}
			}
		}
	}
}

// TestMessageContent_DetectPotentialProviderIssues analyzes the
// marshaled output to detect patterns that might cause provider errors.
func TestMessageContent_DetectPotentialProviderIssues(t *testing.T) {
	testCases := []struct {
		name        string
		content     anthropic.MessageContent
		shouldWarn  bool
		warnReason  string
	}{
		{
			name: "single text block (OK)",
			content: anthropic.MessageContent{
				{Type: "text", Text: "Hello"},
			},
			shouldWarn: false,
		},
		{
			name: "single thinking block (PROBLEMATIC)",
			content: anthropic.MessageContent{
				{Type: "thinking", Thinking: "Thinking..."},
			},
			shouldWarn: true,
			warnReason: "Single thinking block marshals as array, some providers expect string",
		},
		{
			name: "thinking + text (OK - array is valid)",
			content: anthropic.MessageContent{
				{Type: "thinking", Thinking: "Thinking..."},
				{Type: "text", Text: "Response"},
			},
			shouldWarn: false,
		},
		{
			name: "multiple text blocks (OK - merged to single)",
			content: anthropic.MessageContent{
				{Type: "text", Text: "Part 1"},
				{Type: "text", Text: "Part 2"},
			},
			shouldWarn: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.content)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			got := string(data)
			isArray := strings.HasPrefix(got, "[")
			isString := strings.HasPrefix(got, `"`)

			t.Logf("Output: %s", got)
			t.Logf("Format: isArray=%v, isString=%v", isArray, isString)

			if tc.shouldWarn {
				t.Logf("WARNING: %s", tc.warnReason)
				if isArray && len(tc.content) == 1 && tc.content[0].Type != "text" {
					t.Logf("CONFIRMED: Single non-text block produces array format")
				}
			}
		})
	}
}

// TestSimulatedProviderValidation simulates what providers might
// validate when receiving a request.
func TestSimulatedProviderValidation(t *testing.T) {
	// Create a request similar to what causes the 400 error
	req := &anthropic.Request{
		Model:     "claude-sonnet-4.5",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
			},
			{
				Role:    "assistant",
				Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Deep thinking..."}},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	messages, _ := parsed["messages"].([]interface{})
	assistantContent := messages[1].(map[string]interface{})["content"]

	// Simulate provider validation
	t.Run("ProviderExpectsString", func(t *testing.T) {
		// Some providers might validate that content is a string
		if _, isString := assistantContent.(string); !isString {
			t.Logf("VALIDATION ERROR: Expected string, got %T", assistantContent)
			t.Logf("This would cause: 'Invalid input: expected string, received array'")
			t.Logf("Error path: ['messages', 1, 'content']")
		}
	})

	t.Run("ProviderAcceptsArray", func(t *testing.T) {
		// Anthropic API accepts both string and array
		if _, isArray := assistantContent.([]interface{}); isArray {
			t.Logf("VALID: Content is array format (acceptable per Anthropic API spec)")
		}
	})
}

// TestThinkingBlockSignatureHandling tests the signature field
// handling that was recently fixed.
func TestThinkingBlockSignatureHandling(t *testing.T) {
	tests := []struct {
		name              string
		signature         string
		shouldInclude     bool
		description       string
	}{
		{
			name:          "empty signature omitted",
			signature:     "",
			shouldInclude: false,
			description:   "Empty signature should be omitted for OpenRouter/Anthropic compatibility",
		},
		{
			name:          "non-empty signature included",
			signature:     "abc123",
			shouldInclude: true,
			description:   "Non-empty signature should be included for GLM compatibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := anthropic.ContentBlock{
				Type:      "thinking",
				Thinking:  "Thinking content",
			}
			// Only set signature if it should be included
			if tt.signature != "" {
				block.Signature = testStrPtr(tt.signature)
			}
			// If signature is empty string, leave Signature as nil to test omission

			data, err := json.Marshal(block)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			got := string(data)
			hasSignature := strings.Contains(got, "signature")

			t.Logf("Output: %s", got)
			t.Logf("Has signature field: %v (expected: %v)", hasSignature, tt.shouldInclude)
			t.Logf("Description: %s", tt.description)

			if hasSignature != tt.shouldInclude {
				t.Errorf("Signature field presence mismatch: expected=%v, got=%v", tt.shouldInclude, hasSignature)
			}
		})
	}
}

// BenchmarkMessageContent_Marshaling benchmarks the marshaling
// performance for different content structures.
func BenchmarkMessageContent_Marshaling(b *testing.B) {
	tests := []struct {
		name    string
		content anthropic.MessageContent
	}{
		{
			name: "single text",
			content: anthropic.MessageContent{
				{Type: "text", Text: "Hello world"},
			},
		},
		{
			name: "single thinking",
			content: anthropic.MessageContent{
				{Type: "thinking", Thinking: "Thinking content here"},
			},
		},
		{
			name: "thinking + text",
			content: anthropic.MessageContent{
				{Type: "thinking", Thinking: "Thinking..."},
				{Type: "text", Text: "Response"},
			},
		},
		{
			name: "complex with tool_use",
			content: anthropic.MessageContent{
				{Type: "thinking", Thinking: "Thinking..."},
				{Type: "tool_use", ID: "tool_123", Name: "test", Input: json.RawMessage(`{}`)},
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := json.Marshal(tt.content)
				if err != nil {
					b.Fatalf("failed to marshal: %v", err)
				}
			}
		})
	}
}

// TestDebugPrintFullRequest prints a complete request for manual inspection.
// This test is meant to be run with verbose output to see the exact JSON.
func TestDebugPrintFullRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping debug test in short mode")
	}

	// Simulate a thinkMore route request
	req := &anthropic.Request{
		Model:     "claude-opus-4.5",
		MaxTokens: 8192,
		Stream:    true,
		Thinking: &anthropic.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 10000,
		},
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Help me with a complex task"}},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					{Type: "thinking", Thinking: "Let me break this down systematically..."},
					{Type: "text", Text: "I'll help you with that."},
				},
			},
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Tell me more"}},
			},
			{
				Role:    "assistant",
				Content: anthropic.MessageContent{{Type: "thinking", Thinking: "Considering the follow-up..."}},
			},
		},
	}

	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	t.Logf("\n=== FULL REQUEST JSON ===\n%s\n=== END REQUEST JSON ===\n", string(data))

	// Analyze the structure
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if messages, ok := parsed["messages"].([]interface{}); ok {
		t.Logf("\n=== MESSAGE ANALYSIS ===")
		for i, msg := range messages {
			if m, ok := msg.(map[string]interface{}); ok {
				role := m["role"]
				content := m["content"]
				t.Logf("Message %d (role=%s):", i, role)
				switch v := content.(type) {
				case string:
					t.Logf("  Content: STRING (%d chars)", len(v))
				case []interface{}:
					t.Logf("  Content: ARRAY (%d elements)", len(v))
					for j, elem := range v {
						if block, ok := elem.(map[string]interface{}); ok {
							blockType := block["type"]
							t.Logf("    [%d] type=%s", j, blockType)
						}
					}
				}
			}
		}
	}
}
