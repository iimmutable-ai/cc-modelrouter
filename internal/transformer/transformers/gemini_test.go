package transformers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestGeminiTransformer_Name(t *testing.T) {
	tr := NewGeminiTransformer()
	if tr.Name() != "gemini" {
		t.Errorf("expected name 'gemini', got %q", tr.Name())
	}
}

func TestGeminiTransformer_Endpoint(t *testing.T) {
	tr := NewGeminiTransformer()
	expected := "/v1beta/models/%s:generateContent"
	if tr.Endpoint() != expected {
		t.Errorf("expected endpoint %q, got %q", expected, tr.Endpoint())
	}
}

func TestGeminiTransformer_SupportsStreaming(t *testing.T) {
	tr := NewGeminiTransformer()
	if !tr.SupportsStreaming() {
		t.Error("expected SupportsStreaming to return true")
	}
}

func TestGeminiTransformer_ConvertGeminiRole(t *testing.T) {
	tr := NewGeminiTransformer()

	tests := []struct {
		input    string
		expected string
	}{
		{"assistant", "model"},
		{"user", "user"},
		{"system", "system"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tr.convertGeminiRole(tt.input)
			if result != tt.expected {
				t.Errorf("convertGeminiRole(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGeminiTransformer_MapGeminiFinishReason(t *testing.T) {
	tr := NewGeminiTransformer()

	tests := []struct {
		input    string
		expected string
	}{
		{"STOP", "end_turn"},
		{"MAX_TOKENS", "max_tokens"},
		{"SAFETY", "stop_sequence"},
		{"RECITATION", "stop_sequence"},
		{"OTHER", "end_turn"},
		{"UNKNOWN", "UNKNOWN"}, // Unknown values pass through
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tr.mapGeminiFinishReason(tt.input)
			if result != tt.expected {
				t.Errorf("mapGeminiFinishReason(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGeminiTransformer_PrepareRequest_Basic(t *testing.T) {
	tr := NewGeminiTransformer()

	req := &anthropic.Request{
		Model:     "test-model",
		MaxTokens: 100,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
			},
			{
				Role:    "assistant",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hi there"}},
			},
		},
	}

	httpReq, err := tr.PrepareRequest(req, "https://generativelanguage.googleapis.com", "test-api-key", "gemini-1.5-flash")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	// Verify HTTP method
	if httpReq.Method != "POST" {
		t.Errorf("expected POST method, got %q", httpReq.Method)
	}

	// Verify URL contains API key in query
	if !strings.Contains(httpReq.URL.String(), "key=test-api-key") {
		t.Errorf("expected URL to contain 'key=test-api-key', got %q", httpReq.URL.String())
	}

	// Verify URL contains model name
	if !strings.Contains(httpReq.URL.String(), "gemini-1.5-flash") {
		t.Errorf("expected URL to contain model name, got %q", httpReq.URL.String())
	}

	// Verify headers
	if httpReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", httpReq.Header.Get("Content-Type"))
	}

	// Verify GetBody is set for retries
	if httpReq.GetBody == nil {
		t.Error("expected GetBody to be set for retry support")
	}
}

func TestGeminiTransformer_PrepareRequest_WithSystem(t *testing.T) {
	tr := NewGeminiTransformer()

	req := &anthropic.Request{
		Model:     "test-model",
		MaxTokens: 100,
		System:    json.RawMessage("You are a helpful assistant"),
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
			},
		},
	}

	httpReq, err := tr.PrepareRequest(req, "https://generativelanguage.googleapis.com", "test-api-key", "gemini-1.5-flash")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	// Decode body to verify system instruction
	var geminiReq geminiRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&geminiReq); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected SystemInstruction to be set")
	}
	if len(geminiReq.SystemInstruction.Parts) != 1 {
		t.Errorf("expected 1 system instruction part, got %d", len(geminiReq.SystemInstruction.Parts))
	}
	if geminiReq.SystemInstruction.Parts[0].Text != "You are a helpful assistant" {
		t.Errorf("expected system text 'You are a helpful assistant', got %q", geminiReq.SystemInstruction.Parts[0].Text)
	}
}

func TestGeminiTransformer_PrepareRequest_WithTools(t *testing.T) {
	tr := NewGeminiTransformer()

	req := &anthropic.Request{
		Model:     "test-model",
		MaxTokens: 100,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: anthropic.MessageContent{{Type: "text", Text: "Search for info"}},
			},
		},
		Tools: []anthropic.Tool{
			{
				Name:        "search",
				Description: "Search the web",
				InputSchema: map[string]interface{}{"type": "object"},
			},
		},
	}

	httpReq, err := tr.PrepareRequest(req, "https://generativelanguage.googleapis.com", "test-api-key", "gemini-1.5-flash")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	// Decode body to verify tools
	var geminiReq geminiRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&geminiReq); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}

	if len(geminiReq.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(geminiReq.Tools))
	}
	if len(geminiReq.Tools[0].FunctionDeclarations) != 1 {
		t.Errorf("expected 1 function declaration, got %d", len(geminiReq.Tools[0].FunctionDeclarations))
	}
	if geminiReq.Tools[0].FunctionDeclarations[0].Name != "search" {
		t.Errorf("expected tool name 'search', got %q", geminiReq.Tools[0].FunctionDeclarations[0].Name)
	}
}

func TestGeminiTransformer_PrepareRequest_WithImage(t *testing.T) {
	tr := NewGeminiTransformer()

	req := &anthropic.Request{
		Model:     "test-model",
		MaxTokens: 100,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "What is in this image?"},
					{
						Type: "image",
						Source: &anthropic.ImageSource{
							Type:      "base64",
							MediaType: "image/png",
							Data:      "base64imagedata",
						},
					},
				},
			},
		},
	}

	httpReq, err := tr.PrepareRequest(req, "https://generativelanguage.googleapis.com", "test-api-key", "gemini-1.5-flash")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	// Decode body to verify image conversion
	var geminiReq geminiRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&geminiReq); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}

	if len(geminiReq.Contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(geminiReq.Contents))
	}
	if len(geminiReq.Contents[0].Parts) != 2 {
		t.Errorf("expected 2 parts (text + image), got %d", len(geminiReq.Contents[0].Parts))
	}

	// Check image part
	imagePart := geminiReq.Contents[0].Parts[1]
	if imagePart.InlineData == nil {
		t.Fatal("expected InlineData to be set for image")
	}
	if imagePart.InlineData.MimeType != "image/png" {
		t.Errorf("expected mimeType 'image/png', got %q", imagePart.InlineData.MimeType)
	}
	if imagePart.InlineData.Data != "base64imagedata" {
		t.Errorf("expected image data, got %q", imagePart.InlineData.Data)
	}
}

func TestGeminiTransformer_ParseResponse_Success(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create mock Gemini response
	geminiResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{Text: "Hello, how can I help?"},
					},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &geminiUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 20,
			TotalTokenCount:      30,
		},
	}

	// Create mock HTTP response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResp)
	}))
	defer server.Close()

	httpResp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to get mock response: %v", err)
	}

	anthropicResp, err := tr.ParseResponse(httpResp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	// Verify response conversion
	if anthropicResp.Type != "message" {
		t.Errorf("expected type 'message', got %q", anthropicResp.Type)
	}
	if anthropicResp.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", anthropicResp.Role)
	}
	if anthropicResp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", anthropicResp.StopReason)
	}
	if len(anthropicResp.Content) != 1 {
		t.Errorf("expected 1 content block, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", anthropicResp.Content[0].Type)
	}
	if anthropicResp.Content[0].Text != "Hello, how can I help?" {
		t.Errorf("expected text 'Hello, how can I help?', got %q", anthropicResp.Content[0].Text)
	}
	if anthropicResp.Usage.InputTokens != 10 {
		t.Errorf("expected input tokens 10, got %d", anthropicResp.Usage.InputTokens)
	}
	if anthropicResp.Usage.OutputTokens != 20 {
		t.Errorf("expected output tokens 20, got %d", anthropicResp.Usage.OutputTokens)
	}
}

func TestGeminiTransformer_ParseResponse_Error(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create mock error response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid request"))
	}))
	defer server.Close()

	httpResp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to get mock response: %v", err)
	}

	_, err = tr.ParseResponse(httpResp)
	if err == nil {
		t.Error("expected error for bad status code")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got %v", err)
	}
}

func TestGeminiTransformer_ParseResponse_BlockedContent(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create mock Gemini response with blocked content
	geminiResp := geminiResponse{
		Candidates: []geminiCandidate{},
		PromptFeedback: &geminiPromptFeedback{
			BlockReason: "SAFETY",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResp)
	}))
	defer server.Close()

	httpResp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to get mock response: %v", err)
	}

	_, err = tr.ParseResponse(httpResp)
	if err == nil {
		t.Error("expected error for blocked content")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected error to contain 'blocked', got %v", err)
	}
}

func TestGeminiTransformer_ParseResponse_ToolCall(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create mock Gemini response with tool call
	geminiResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{
							FunctionCall: &geminiFunctionCall{
								Name: "search",
								Args: map[string]interface{}{"query": "test"},
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResp)
	}))
	defer server.Close()

	httpResp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to get mock response: %v", err)
	}

	anthropicResp, err := tr.ParseResponse(httpResp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	// Verify tool_use content block
	if len(anthropicResp.Content) != 1 {
		t.Errorf("expected 1 content block, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content[0].Type != "tool_use" {
		t.Errorf("expected content type 'tool_use', got %q", anthropicResp.Content[0].Type)
	}
	if anthropicResp.Content[0].Name != "search" {
		t.Errorf("expected tool name 'search', got %q", anthropicResp.Content[0].Name)
	}
	if !strings.Contains(anthropicResp.Content[0].ID, "search") {
		t.Errorf("expected ID to contain 'search', got %q", anthropicResp.Content[0].ID)
	}
}

func TestGeminiTransformer_TransformStreamEvent_TextDelta(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create Gemini streaming chunk
	geminiChunk := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "Hello"},
					},
				},
			},
		},
	}

	data, _ := json.Marshal(geminiChunk)
	event := &transformer.SSEEvent{
		EventType: "chunk",
		Data:      data,
	}

	events, err := tr.TransformStreamEvent(event)
	if err != nil {
		t.Fatalf("TransformStreamEvent failed: %v", err)
	}

	// Should generate message_start and content_block_delta
	if len(events) < 1 {
		t.Errorf("expected at least 1 event, got %d", len(events))
	}

	// Check for content_block_delta (message_start may or may not be present)
	var foundContentDelta bool
	for _, ev := range events {
		if ev.EventType == "content_block_delta" {
			foundContentDelta = true
			// Verify delta contains text
			var delta map[string]interface{}
			if err := json.Unmarshal(ev.Data, &delta); err != nil {
				t.Fatalf("failed to unmarshal delta: %v", err)
			}
			if delta["type"] != "content_block_delta" {
				t.Errorf("expected type 'content_block_delta', got %v", delta["type"])
			}
		}
	}

	if !foundContentDelta {
		t.Error("expected content_block_delta event")
	}
	// Note: message_start is generated on first content, but may not be in this chunk
	// if tr.messageStarted was already true from previous test
}

func TestGeminiTransformer_TransformStreamEvent_FinishReason(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create Gemini streaming chunk with finish reason
	geminiChunk := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     10,
			"candidatesTokenCount": 20,
		},
	}

	data, _ := json.Marshal(geminiChunk)
	event := &transformer.SSEEvent{
		EventType: "chunk",
		Data:      data,
	}

	events, err := tr.TransformStreamEvent(event)
	if err != nil {
		t.Fatalf("TransformStreamEvent failed: %v", err)
	}

	// Should generate content_block_stop, message_delta, message_stop
	var foundContentStop, foundMessageDelta, foundMessageStop bool
	for _, ev := range events {
		switch ev.EventType {
		case "content_block_stop":
			foundContentStop = true
		case "message_delta":
			foundMessageDelta = true
			// Verify it contains usage
			var delta map[string]interface{}
			if err := json.Unmarshal(ev.Data, &delta); err != nil {
				t.Fatalf("failed to unmarshal message_delta: %v", err)
			}
			if usage, ok := delta["usage"].(map[string]interface{}); ok {
				// JSON unmarshals numbers as float64
				outputTokens := int(usage["output_tokens"].(float64))
				if outputTokens != 20 {
					t.Errorf("expected output_tokens 20, got %v", outputTokens)
				}
			} else {
				t.Error("expected message_delta to have usage")
			}
		case "message_stop":
			foundMessageStop = true
		}
	}

	if !foundContentStop {
		t.Error("expected content_block_stop event")
	}
	if !foundMessageDelta {
		t.Error("expected message_delta event")
	}
	if !foundMessageStop {
		t.Error("expected message_stop event")
	}
}

func TestGeminiTransformer_TransformStreamEvent_PassThrough(t *testing.T) {
	tr := NewGeminiTransformer()

	// Create event that can't be parsed as Gemini (should pass through)
	event := &transformer.SSEEvent{
		EventType: "custom",
		Data:      []byte(`{"invalid": true}`),
	}

	events, err := tr.TransformStreamEvent(event)
	if err != nil {
		t.Fatalf("TransformStreamEvent failed: %v", err)
	}

	// Should pass through unchanged
	if len(events) != 1 {
		t.Errorf("expected 1 event (pass-through), got %d", len(events))
	}
	if events[0].EventType != "custom" {
		t.Errorf("expected event type 'custom', got %q", events[0].EventType)
	}
}

func TestGeminiTransformer_ResetState(t *testing.T) {
	tr := NewGeminiTransformer()

	// The messageStarted flag is managed internally during streaming.
	// We test that the resetState method works correctly.
	tr.messageStarted = true
	tr.resetState()

	if tr.messageStarted {
		t.Error("expected messageStarted to be false after resetState()")
	}
}