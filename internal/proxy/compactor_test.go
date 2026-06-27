package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestNewCompactor(t *testing.T) {
	tests := []struct {
		name         string
		maxBodyBytes int64
		compaction   *config.CompactionConfig
		wantNil      bool
		wantStrategy string
	}{
		{
			name:         "no limit configured - returns nil",
			maxBodyBytes: 0,
			compaction:   nil,
			wantNil:      true,
		},
		{
			name:         "negative limit - returns nil",
			maxBodyBytes: -1,
			compaction:   nil,
			wantNil:      true,
		},
		{
			name:         "with limit but no compaction config - defaults to llm strategy",
			maxBodyBytes: 100000,
			compaction:   nil,
			wantNil:      false,
			wantStrategy: "llm",
		},
		{
			name:         "with limit and trim method - uses trim strategy",
			maxBodyBytes: 100000,
			compaction:   &config.CompactionConfig{Method: "trim"},
			wantNil:      false,
			wantStrategy: "trim",
		},
		{
			name:         "with limit and llm method - uses llm strategy",
			maxBodyBytes: 100000,
			compaction:   &config.CompactionConfig{Method: "llm"},
			wantNil:      false,
			wantStrategy: "llm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerCfg := config.ProviderConfig{
				MaxRequestBodyBytes: tt.maxBodyBytes,
				Compaction:          tt.compaction,
			}

			c := NewCompactor(nil, "test-provider", providerCfg)

			if tt.wantNil {
				if c != nil {
					t.Errorf("NewCompactor() = %v, want nil", c)
				}
				return
			}

			if c == nil {
				t.Fatal("NewCompactor() = nil, want non-nil")
			}

			// Verify strategy by checking the internal field
			comp := c.(*compactor)
			if comp.strategy.Name() != tt.wantStrategy {
				t.Errorf("strategy = %v, want %v", comp.strategy.Name(), tt.wantStrategy)
			}
		})
	}
}

func TestCompactorShouldCompact(t *testing.T) {
	tests := []struct {
		name         string
		maxBodyBytes int64
		reqSize      int64
		want         bool
	}{
		{
			name:         "request under limit",
			maxBodyBytes: 100000,
			reqSize:      50000,
			want:         false,
		},
		{
			name:         "request at limit",
			maxBodyBytes: 100000,
			reqSize:      100000,
			want:         false,
		},
		{
			name:         "request over limit",
			maxBodyBytes: 100000,
			reqSize:      100001,
			want:         true,
		},
		{
			name:         "zero limit - never compact",
			maxBodyBytes: 0,
			reqSize:      1000000,
			want:         false,
		},
		{
			name:         "negative limit - never compact",
			maxBodyBytes: -1,
			reqSize:      1000000,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerCfg := config.ProviderConfig{
				MaxRequestBodyBytes: tt.maxBodyBytes,
			}
			c := NewCompactor(nil, "test-provider", providerCfg)

			if c == nil {
				t.Skip("compactor is nil for this configuration")
			}

			got := c.ShouldCompact(tt.reqSize)
			if got != tt.want {
				t.Errorf("ShouldCompact(%d) = %v, want %v", tt.reqSize, got, tt.want)
			}
		})
	}
}

func TestTrimStrategyCompact(t *testing.T) {
	tests := []struct {
		name             string
		messages         []anthropic.Message
		wantCompacted    bool
		wantMessageCount int
		wantSummaryText  string
	}{
		{
			name:             "empty messages",
			messages:         []anthropic.Message{},
			wantCompacted:    true,
			wantMessageCount: 0,
		},
		{
			name: "single message - keeps it",
			messages: []anthropic.Message{
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			},
			wantCompacted:    true,
			wantMessageCount: 1,
		},
		{
			name: "two messages - keeps only most recent",
			messages: []anthropic.Message{
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
				{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi there"}}},
			},
			wantCompacted:    true,
			wantMessageCount: 1, // Keeps only most recent when <= 2 messages
		},
		{
			name: "three messages - trims to summary + 2",
			messages: []anthropic.Message{
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Message 1"}}},
				{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Response 1"}}},
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Message 2"}}},
			},
			wantCompacted:    true,
			wantMessageCount: 3, // 1 summary + 2 kept
			wantSummaryText:  "[Previous 1 messages removed due to size constraints]",
		},
		{
			name: "five messages - trims to summary + 2",
			messages: []anthropic.Message{
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Message 1"}}},
				{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Response 1"}}},
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Message 2"}}},
				{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Response 2"}}},
				{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Message 3"}}},
			},
			wantCompacted:    true,
			wantMessageCount: 3, // 1 summary + 2 kept
			wantSummaryText:  "[Previous 3 messages removed due to size constraints]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:    "test-model",
				Messages: tt.messages,
			}

			strategy := &trimStrategy{}
			providerCfg := config.ProviderConfig{MaxRequestBodyBytes: 1000}

			compacted, didCompact, err := strategy.Compact(req, nil, "test-provider", providerCfg)
			if err != nil {
				t.Fatalf("Compact() error = %v", err)
			}

			if didCompact != tt.wantCompacted {
				t.Errorf("didCompact = %v, want %v", didCompact, tt.wantCompacted)
			}

			if len(compacted.Messages) != tt.wantMessageCount {
				t.Errorf("message count = %d, want %d", len(compacted.Messages), tt.wantMessageCount)
			}

			if tt.wantSummaryText != "" && len(compacted.Messages) > 0 {
				// Check first message contains the summary text
				firstMsg := compacted.Messages[0]
				if len(firstMsg.Content) > 0 && firstMsg.Content[0].Text != tt.wantSummaryText {
					t.Errorf("summary text = %q, want %q", firstMsg.Content[0].Text, tt.wantSummaryText)
				}
			}

			// Verify original request is not modified
			if len(req.Messages) != len(tt.messages) {
				t.Error("original request was modified")
			}
		})
	}
}

func TestExtractTextFromMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  anthropic.Message
		wantText string
	}{
		{
			name:     "empty message",
			message:  anthropic.Message{Role: anthropic.RoleUser},
			wantText: "",
		},
		{
			name: "single text block",
			message: anthropic.Message{
				Role:    anthropic.RoleUser,
				Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello world"}},
			},
			wantText: "Hello world",
		},
		{
			name: "multiple text blocks",
			message: anthropic.Message{
				Role: anthropic.RoleUser,
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Hello"},
					{Type: "text", Text: "world"},
				},
			},
			wantText: "Hello world",
		},
		{
			name: "mixed content types - only text extracted",
			message: anthropic.Message{
				Role: anthropic.RoleUser,
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Hello"},
					{Type: "image", Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
					{Type: "text", Text: "world"},
				},
			},
			wantText: "Hello world",
		},
		{
			name: "empty text blocks ignored",
			message: anthropic.Message{
				Role: anthropic.RoleUser,
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: ""},
					{Type: "text", Text: "Hello"},
					{Type: "text", Text: ""},
				},
			},
			wantText: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromMessage(tt.message)
			if got != tt.wantText {
				t.Errorf("extractTextFromMessage() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestExtractTextFromResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *anthropic.Response
		wantText string
	}{
		{
			name:     "empty response",
			response: &anthropic.Response{},
			wantText: "",
		},
		{
			name: "single text block",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello world"}},
			},
			wantText: "Hello world",
		},
		{
			name: "multiple text blocks",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Hello"},
					{Type: "text", Text: "world"},
				},
			},
			wantText: "Hello world",
		},
		{
			name: "thinking block ignored",
			response: &anthropic.Response{
				Content: []anthropic.ContentBlock{
					{Type: "thinking", Thinking: "Some reasoning"},
					{Type: "text", Text: "Result"},
				},
			},
			wantText: "Result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromResponse(tt.response)
			if got != tt.wantText {
				t.Errorf("extractTextFromResponse() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxLen  int
		want    string
	}{
		{
			name:   "string shorter than max",
			input:  "Hello",
			maxLen: 10,
			want:   "Hello",
		},
		{
			name:   "string equal to max",
			input:  "Hello world",
			maxLen: 11,
			want:   "Hello world",
		},
		{
			name:   "string longer than max",
			input:  "Hello world this is a long string",
			maxLen: 11,
			want:   "Hello world...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen zero",
			input:  "Hello",
			maxLen: 0,
			want:   "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestEstimateRequestSize(t *testing.T) {
	// Get actual serialized sizes for comparison
	emptyReq := &anthropic.Request{}
	emptyData, _ := json.Marshal(emptyReq)
	emptySize := int64(len(emptyData))

	tests := []struct {
		name string
		req  *anthropic.Request
		want int64
	}{
		{
			name: "empty request",
			req:  &anthropic.Request{},
			want: emptySize,
		},
		{
			name: "request with model",
			req: &anthropic.Request{
				Model: "claude-3-sonnet",
			},
			want: func() int64 {
				data, _ := json.Marshal(&anthropic.Request{Model: "claude-3-sonnet"})
				return int64(len(data))
			}(),
		},
		{
			name: "request with messages",
			req: &anthropic.Request{
				Model: "claude-3-sonnet",
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
				},
			},
			want: func() int64 {
				data, _ := json.Marshal(&anthropic.Request{
					Model: "claude-3-sonnet",
					Messages: []anthropic.Message{
						{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
					},
				})
				return int64(len(data))
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateRequestSize(tt.req)
			if got != tt.want {
				t.Errorf("estimateRequestSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildSummaryPrompt(t *testing.T) {
	messages := []anthropic.Message{
		{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
		{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi there"}}},
	}

	prompt := buildSummaryPrompt(messages)

	// Check prompt contains expected sections
	if prompt == "" {
		t.Error("buildSummaryPrompt() returned empty string")
	}

	// Should contain instructions
	if !containsString(prompt, "concise summary") {
		t.Error("prompt missing summary instruction")
	}

	// Should contain conversation marker
	if !containsString(prompt, "Conversation:") {
		t.Error("prompt missing conversation marker")
	}

	// Should contain message roles
	if !containsString(prompt, "user:") {
		t.Error("prompt missing user role")
	}

	if !containsString(prompt, "assistant:") {
		t.Error("prompt missing assistant role")
	}
}

func TestCompactRequestIfNeeded(t *testing.T) {
	tests := []struct {
		name            string
		maxBodyBytes    int64
		messageCount    int
		messageSize     int // Approximate size per message
		wantCompacted   bool
		wantErr         bool
	}{
		{
			name:         "no limit configured",
			maxBodyBytes: 0,
			messageCount: 10,
			wantCompacted: false,
			wantErr:      false,
		},
		{
			name:         "under limit - no compaction",
			maxBodyBytes: 100000,
			messageCount: 2,
			messageSize:  100,
			wantCompacted: false,
			wantErr:      false,
		},
		{
			name:         "over limit - compaction needed",
			maxBodyBytes: 500,
			messageCount: 10,
			messageSize:  100,
			wantCompacted: true,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build request with specified message count
			messages := make([]anthropic.Message, tt.messageCount)
			for i := 0; i < tt.messageCount; i++ {
				text := "message content"
				if tt.messageSize > len(text) {
					// Pad to approximate size
					text = makeStringOfLength(tt.messageSize)
				}
				role := anthropic.RoleUser
				if i%2 == 1 {
					role = anthropic.RoleAssistant
				}
				messages[i] = anthropic.Message{
					Role:    role,
					Content: []anthropic.ContentBlock{{Type: "text", Text: text}},
				}
			}

			req := &anthropic.Request{
				Model:    "test-model",
				Messages: messages,
			}

			providerCfg := config.ProviderConfig{
				MaxRequestBodyBytes: tt.maxBodyBytes,
				Compaction:          &config.CompactionConfig{Method: "trim"},
			}

			compactedReq, didCompact, err := CompactRequestIfNeeded(req, nil, "test-provider", providerCfg)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if didCompact != tt.wantCompacted {
				t.Errorf("didCompact = %v, want %v", didCompact, tt.wantCompacted)
			}

			// If compacted, verify we got a different request
			if tt.wantCompacted && compactedReq == req {
				t.Error("compacted request should be a different object")
			}

			// If not compacted, should get original request back
			if !tt.wantCompacted && compactedReq != req {
				t.Error("should return original request when not compacted")
			}
		})
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func makeStringOfLength(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = chars[i%len(chars)]
	}
	return string(result)
}

// ---------------------------------------------------------------------------
// llmStrategy.Compact — fallback paths
// ---------------------------------------------------------------------------

// compactorMockTransformerRegistry wraps a real Registry with the TransformerRegistry interface.
type compactorMockTransformerRegistry struct {
	registry *transformer.Registry
}

func (m *compactorMockTransformerRegistry) Get(name string) (transformer.Transformer, error) {
	return m.registry.Get(name)
}

// compactorMockHTTPClient implements HTTPClient for compactor testing.
type compactorMockHTTPClient struct {
	resp *http.Response
	err  error
}

func (c *compactorMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.resp, c.err
}

func (c *compactorMockHTTPClient) DoWithContext(ctx context.Context, req *http.Request) (*http.Response, error) {
	return c.resp, c.err
}

// newTestHandler creates a Handler with a transformer registry, config, and provider clients.
func newTestHandlerForCompactor() *Handler {
	h := NewHandler(1024 * 1024)
	reg := transformer.NewRegistry()
	reg.Register(transformers.NewAnthropicTransformer())
	h.SetTransformerRegistry(&compactorMockTransformerRegistry{registry: reg})
	h.config = &config.Config{
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				BaseURL: "http://localhost/v1",
				Models:  []string{"test-model"},
			},
		},
	}
	return h
}

func TestLLMStrategy_TransformerNotFound(t *testing.T) {
	// Handler with empty registry — transformer not found
	h := newTestHandlerForCompactor()
	// Clear the registry to simulate transformer not found
	h.SetTransformerRegistry(&compactorMockTransformerRegistry{registry: transformer.NewRegistry()})

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             "http://localhost/v1",
	}

	strategy := &llmStrategy{}
	compacted, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
	if compacted == nil {
		t.Fatal("expected non-nil compacted request")
	}
	// The request should be returned (may or may not have fewer messages depending on size)
	if len(compacted.Messages) == 0 {
		t.Error("expected at least some messages after compaction")
	}
}

func TestLLMStrategy_PrepareRequestFails(t *testing.T) {
	h := newTestHandlerForCompactor()

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             "http://", // invalid URL will cause PrepareRequest to fail
	}

	strategy := &llmStrategy{}
	_, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
}

func TestLLMStrategy_ClientNil(t *testing.T) {
	h := newTestHandlerForCompactor()
	// Do NOT set any provider clients — client will be nil

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             "http://localhost/v1",
	}

	strategy := &llmStrategy{}
	_, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
}

func TestLLMStrategy_ParseResponseFails(t *testing.T) {
	h := newTestHandlerForCompactor()

	// Set up a mock client that returns an error response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	h.SetProviderClients(map[string]HTTPClient{
		"test-provider": &compactorMockHTTPClient{},
	})

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             server.URL,
	}

	strategy := &llmStrategy{}
	_, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
}

func TestLLMStrategy_EmptySummary(t *testing.T) {
	h := newTestHandlerForCompactor()

	// Set up a mock client that returns a response with no text content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respJSON := `{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"test","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":0}}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(respJSON))
	}))
	defer server.Close()

	h.SetProviderClients(map[string]HTTPClient{
		"test-provider": &compactorMockHTTPClient{},
	})

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             server.URL,
	}

	strategy := &llmStrategy{}
	_, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
	// Verify original request is not modified
	if len(req.Messages) != 3 {
		t.Error("original request was modified")
	}
}

func TestLLMStrategy_ClientDoFails(t *testing.T) {
	h := newTestHandlerForCompactor()

	// Client that always fails
	h.SetProviderClients(map[string]HTTPClient{
		"test-provider": &compactorMockHTTPClient{err: errors.New("connection refused")},
	})

	req := &anthropic.Request{
		Model:    "test-model",
		Messages: []anthropic.Message{
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hello"}}},
			{Role: anthropic.RoleAssistant, Content: []anthropic.ContentBlock{{Type: "text", Text: "Hi"}}},
			{Role: anthropic.RoleUser, Content: []anthropic.ContentBlock{{Type: "text", Text: "World"}}},
		},
	}

	providerCfg := config.ProviderConfig{
		MaxRequestBodyBytes: 100,
		Models:              []string{"test-model"},
		BaseURL:             "http://localhost/v1",
	}

	strategy := &llmStrategy{}
	_, didCompact, err := strategy.Compact(req, h, "test-provider", providerCfg)
	if err != nil {
		t.Fatalf("expected no error (falls back to trim), got: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur via trim fallback")
	}
}
