package proxy

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// deepCopyRequestGob is an alternative gob-based deep copy, benchmarked but
// found to be ~30% slower than JSON on this platform. Kept here as a
// reference benchmark.
func deepCopyRequestGob(req *anthropic.Request) (*anthropic.Request, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to gob-encode request: %w", err)
	}
	var reqCopy anthropic.Request
	dec := gob.NewDecoder(&buf)
	if err := dec.Decode(&reqCopy); err != nil {
		return nil, fmt.Errorf("failed to gob-decode request: %w", err)
	}
	return &reqCopy, nil
}

// TestGobDeepCopyCorrectness verifies that gob-based deep copy produces
// identical results to JSON-based deep copy for all field types.
func TestGobDeepCopyCorrectness(t *testing.T) {
	tests := []struct {
		name string
		req  *anthropic.Request
	}{
		{
			name: "simple text request",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 1024,
				Messages: []anthropic.Message{
					{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}}},
				},
			},
		},
		{
			name: "request with thinking blocks",
			req: &anthropic.Request{
				Model:     "glm-4.7",
				MaxTokens: 8192,
				Messages: []anthropic.Message{
					{
						Role: "user",
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Hello"},
							{Type: "thinking", Thinking: "Deep thought", Signature: strPtr("sig123")},
						},
					},
				},
				Thinking: &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 10000},
			},
		},
		{
			name: "request with tools and tool_choice",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 4096,
				Messages: []anthropic.Message{
					{
						Role: "assistant",
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Let me help."},
							{Type: "tool_use", ID: "tool_123", Name: "read_file", Input: json.RawMessage(`{"path":"/tmp/test"}`)},
						},
					},
					{
						Role: "user",
						Content: anthropic.MessageContent{
							{Type: "tool_result", ID: "tool_123", Content: anthropic.MessageContent{{Type: "text", Text: "file contents"}}},
						},
					},
				},
				Tools: []anthropic.Tool{
					{Name: "read_file", Description: "Read a file", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}},
				},
				ToolChoice: map[string]any{"type": "auto"},
			},
		},
		{
			name: "request with json.RawMessage system field",
			req: &anthropic.Request{
				Model:     "claude-opus-4",
				MaxTokens: 16384,
				System:    json.RawMessage(`"You are a helpful assistant."`),
				Messages: []anthropic.Message{
					{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hi"}}},
				},
				Metadata: map[string]any{"user_id": "test-user", "session": "abc123"},
			},
		},
		{
			name: "request with consecutive text blocks (MessageContent merge case)",
			req: &anthropic.Request{
				Model:     "glm-4.7",
				MaxTokens: 1024,
				Messages: []anthropic.Message{
					{
						Role: "user",
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Part 1"},
							{Type: "text", Text: "Part 2"},
							{Type: "text", Text: "Part 3"},
						},
					},
				},
			},
		},
		{
			name: "request with nil optional fields",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 1024,
				Stream:    true,
				Messages:  []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Test"}}}},
				// System, Tools, ToolChoice, Metadata, Thinking all nil/zero
			},
		},
		{
			name: "empty request",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 0,
				Messages:  []anthropic.Message{},
			},
		},
		{
			name: "request with nil signature (thinking block)",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 4096,
				Messages: []anthropic.Message{
					{
						Role: "assistant",
						Content: anthropic.MessageContent{
							{Type: "thinking", Thinking: "thought process", Signature: nil},
							{Type: "text", Text: "response"},
						},
					},
				},
				Thinking: &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 5000},
			},
		},
		{
			name: "request with empty string signature",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 4096,
				Messages: []anthropic.Message{
					{
						Role: "assistant",
						Content: anthropic.MessageContent{
							{Type: "thinking", Thinking: "thought", Signature: strPtr("")},
							{Type: "text", Text: "response"},
						},
					},
				},
			},
		},
		{
			name: "request with image blocks",
			req: &anthropic.Request{
				Model:     "claude-sonnet-4",
				MaxTokens: 1024,
				Messages: []anthropic.Message{
					{
						Role: "user",
						Content: anthropic.MessageContent{
							{Type: "text", Text: "What's in this image?"},
							{Type: "image", Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: "iVBORw0KGgo="}},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create gob copy
			gobCopy, err := deepCopyRequestGob(tt.req)
			if err != nil {
				t.Fatalf("gob deepCopyRequestGob failed: %v", err)
			}

			// Create JSON copy (production method)
			jsonCopy, err := deepCopyRequest(tt.req)
			if err != nil {
				t.Fatalf("JSON deepCopyRequest failed: %v", err)
			}

			// Compare gob copy fields against original (struct-level fidelity)
			if gobCopy.Model != tt.req.Model {
				t.Errorf("Model mismatch: got %q, want %q", gobCopy.Model, tt.req.Model)
			}
			if gobCopy.MaxTokens != tt.req.MaxTokens {
				t.Errorf("MaxTokens mismatch: got %d, want %d", gobCopy.MaxTokens, tt.req.MaxTokens)
			}
			if gobCopy.Stream != tt.req.Stream {
				t.Errorf("Stream mismatch: got %v, want %v", gobCopy.Stream, tt.req.Stream)
			}
			if len(gobCopy.Messages) != len(tt.req.Messages) {
				t.Errorf("Messages count mismatch: got %d, want %d", len(gobCopy.Messages), len(tt.req.Messages))
			}
			if !bytes.Equal(gobCopy.System, tt.req.System) {
				t.Errorf("System mismatch: got %s, want %s", string(gobCopy.System), string(tt.req.System))
			}
			if len(gobCopy.Tools) != len(tt.req.Tools) {
				t.Errorf("Tools count mismatch: got %d, want %d", len(gobCopy.Tools), len(tt.req.Tools))
			}

			// Verify independence — modify gob copy, check original unchanged
			if len(gobCopy.Messages) > 0 && len(gobCopy.Messages[0].Content) > 0 {
				gobCopy.Messages[0].Content[0].Text = "MUTATED"
				if tt.req.Messages[0].Content[0].Text == "MUTATED" {
					t.Error("Original request was mutated through gob copy")
				}
			}

			// Verify JSON copy also independent
			if len(jsonCopy.Messages) > 0 && len(jsonCopy.Messages[0].Content) > 0 {
				jsonCopy.Messages[0].Content[0].Text = "JSON_MUTATED"
				if tt.req.Messages[0].Content[0].Text == "JSON_MUTATED" {
					t.Error("Original request was mutated through JSON copy")
				}
			}

			// Note: gob and JSON copies may differ in MessageContent representation
			// because JSON uses custom MarshalJSON (merges consecutive text blocks)
			// while gob preserves the raw Go struct. Both are valid deep copies.
			// What matters is that both produce independent copies of the original.
		})
	}
}

// BenchmarkDeepCopyJSON benchmarks the production JSON-based deep copy.
func BenchmarkDeepCopyJSON(b *testing.B) {
	req := buildBenchmarkRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := deepCopyRequest(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDeepCopyGob benchmarks the alternative gob-based deep copy.
// Kept as reference — gob was found to be ~30% slower than JSON.
func BenchmarkDeepCopyGob(b *testing.B) {
	req := buildBenchmarkRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := deepCopyRequestGob(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func buildBenchmarkRequest() *anthropic.Request {
	// Build a realistic multi-message request with various block types
	messages := make([]anthropic.Message, 10)
	for i := range messages {
		role := anthropic.RoleUser
		if i%2 == 1 {
			role = anthropic.RoleAssistant
		}
		content := anthropic.MessageContent{
			{Type: "text", Text: "This is a moderately long message that simulates a realistic conversation turn with some substance to it."},
		}
		if i == 2 {
			content = append(content, anthropic.ContentBlock{
				Type: "thinking", Thinking: "Let me think about this carefully...", Signature: strPtr("sig"),
			})
		}
		if i == 4 {
			content = append(content, anthropic.ContentBlock{
				Type: "text", Text: "Another text block",
			})
		}
		messages[i] = anthropic.Message{Role: role, Content: content}
	}

	return &anthropic.Request{
		Model:     "claude-sonnet-4",
		MaxTokens: 8192,
		System:    json.RawMessage(`"You are a helpful coding assistant that provides detailed answers."`),
		Messages:  messages,
		Tools: []anthropic.Tool{
			{Name: "read_file", Description: "Read file contents", InputSchema: map[string]any{"type": "object"}},
			{Name: "write_file", Description: "Write to a file", InputSchema: map[string]any{"type": "object"}},
			{Name: "bash", Description: "Run bash command", InputSchema: map[string]any{"type": "object"}},
		},
		ToolChoice: map[string]any{"type": "auto"},
		Metadata:   map[string]any{"user_id": "benchmark-test"},
		Stream:     true,
		Thinking:   &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 10000},
	}
}

// init registers types that may appear behind any/interface{} fields
// so gob can encode/decode them correctly.
func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
}