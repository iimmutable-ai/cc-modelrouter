package transformer_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestAnthropicTransformer tests the Anthropic transformer with the new interface.
func TestAnthropicTransformer(t *testing.T) {
	tr := transformers.NewAnthropicTransformer()

	// Test request transformation
	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 1024,
		Stream:    false,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello, how are you?"},
				},
			},
		},
	}

	// Test PrepareRequest
	httpReq, err := tr.PrepareRequest(req, "https://api.anthropic.com", "sk-test", "claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	if httpReq.Method != "POST" {
		t.Errorf("HTTP method mismatch: got %s, want POST", httpReq.Method)
	}
	if httpReq.Header.Get("x-api-key") != "sk-test" {
		t.Errorf("API key header mismatch: got %s, want sk-test", httpReq.Header.Get("x-api-key"))
	}

	// Test with mock response
	anthropicResp := &anthropic.Response{
		ID:         "msg_123",
		Type:       "message",
		Role:       anthropic.RoleAssistant,
		Model:      "claude-3-5-sonnet-20241022",
		StopReason: "end_turn",
		Content: []anthropic.ContentBlock{
			{Type: "text", Text: "I'm doing well, thank you!"},
		},
		Usage: anthropic.Usage{
			InputTokens:  10,
			OutputTokens: 8,
		},
	}

	respBody, _ := json.Marshal(anthropicResp)
	mockResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(respBody))),
	}

	// Test ParseResponse
	parsedResp, err := tr.ParseResponse(mockResp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	if parsedResp.ID != anthropicResp.ID {
		t.Errorf("ID mismatch: got %s, want %s", parsedResp.ID, anthropicResp.ID)
	}
	if parsedResp.StopReason != anthropicResp.StopReason {
		t.Errorf("StopReason mismatch: got %s, want %s", parsedResp.StopReason, anthropicResp.StopReason)
	}
	if len(parsedResp.Content) == 0 {
		t.Error("Expected content in response")
	}
	if parsedResp.Content[0].Text != anthropicResp.Content[0].Text {
		t.Errorf("Content mismatch: got %s, want %s", parsedResp.Content[0].Text, anthropicResp.Content[0].Text)
	}
}

// TestOpenAITransformer tests the OpenAI transformer with the new interface.
func TestOpenAITransformer(t *testing.T) {
	tr := transformers.NewOpenAITransformer()

	// Test request transformation
	req := &anthropic.Request{
		Model:     "gpt-4",
		MaxTokens: 1024,
		Stream:    false,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello, how are you?"},
				},
			},
		},
	}

	// Test PrepareRequest
	httpReq, err := tr.PrepareRequest(req, "https://api.openai.com", "sk-test", "gpt-4")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	if httpReq.Method != "POST" {
		t.Errorf("HTTP method mismatch: got %s, want POST", httpReq.Method)
	}
	if httpReq.Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("Authorization header mismatch: got %s, want Bearer sk-test", httpReq.Header.Get("Authorization"))
	}

	// Test with mock OpenAI response
	openaiResp := map[string]any{
		"id":      "chatcmpl-123",
		"object":  "chat.completion",
		"created": 1234567890,
		"model":   "gpt-4",
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "I'm doing well, thank you!",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 8,
			"total_tokens":      18,
		},
	}

	respBody, _ := json.Marshal(openaiResp)
	mockResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(respBody))),
	}

	// Test ParseResponse
	parsedResp, err := tr.ParseResponse(mockResp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	if parsedResp.ID != "chatcmpl-123" {
		t.Errorf("ID mismatch: got %s, want chatcmpl-123", parsedResp.ID)
	}
	if parsedResp.StopReason != "end_turn" {
		t.Errorf("StopReason mismatch: got %s, want end_turn", parsedResp.StopReason)
	}
	if len(parsedResp.Content) == 0 {
		t.Error("Expected content in response")
	}
	if parsedResp.Content[0].Text != "I'm doing well, thank you!" {
		t.Errorf("Content mismatch: got %s, want 'I'm doing well, thank you!'", parsedResp.Content[0].Text)
	}
}

// TestGeminiTransformer tests the Gemini transformer with the new interface.
func TestGeminiTransformer(t *testing.T) {
	tr := transformers.NewGeminiTransformer()

	// Test request transformation
	req := &anthropic.Request{
		Model:     "gemini-pro",
		MaxTokens: 1024,
		Stream:    false,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello, how are you?"},
				},
			},
		},
	}

	// Test PrepareRequest
	httpReq, err := tr.PrepareRequest(req, "https://generativelanguage.googleapis.com", "test-key", "gemini-pro")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	if httpReq.Method != "POST" {
		t.Errorf("HTTP method mismatch: got %s, want POST", httpReq.Method)
	}
	if !strings.Contains(httpReq.URL.String(), "gemini-pro:generateContent") {
		t.Errorf("URL should contain gemini-pro:generateContent, got %s", httpReq.URL.String())
	}

	// Test with mock Gemini response
	geminiResp := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"parts": []any{
						map[string]any{
							"text": "I'm doing well, thank you!",
						},
					},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]any{
			"promptTokenCount":     10,
			"candidatesTokenCount": 8,
			"totalTokenCount":      18,
		},
	}

	respBody, _ := json.Marshal(geminiResp)
	mockResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(respBody))),
	}

	// Test ParseResponse
	parsedResp, err := tr.ParseResponse(mockResp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	if parsedResp.StopReason != "end_turn" {
		t.Errorf("StopReason mismatch: got %s, want end_turn", parsedResp.StopReason)
	}
	if len(parsedResp.Content) == 0 {
		t.Error("Expected content in response")
	}
	if parsedResp.Content[0].Text != "I'm doing well, thank you!" {
		t.Errorf("Content mismatch: got %s, want 'I'm doing well, thank you!'", parsedResp.Content[0].Text)
	}
}

// TestAllProvidersSupportStreaming verifies all providers support streaming.
func TestAllProvidersSupportStreaming(t *testing.T) {
	providersList := []struct {
		name string
		tr   transformer.Transformer
	}{
		{"anthropic", transformers.NewAnthropicTransformer()},
		{"openai", transformers.NewOpenAITransformer()},
		{"gemini", transformers.NewGeminiTransformer()},
	}

	for _, p := range providersList {
		t.Run(p.name, func(t *testing.T) {
			if !p.tr.SupportsStreaming() {
				t.Errorf("Provider %s should support streaming", p.name)
			}
		})
	}
}

// TestAllProvidersHaveValidEndpoints verifies all providers have valid endpoints.
func TestAllProvidersHaveValidEndpoints(t *testing.T) {
	providersList := []struct {
		name           string
		tr             transformer.Transformer
		expectedPrefix string
	}{
		{"anthropic", transformers.NewAnthropicTransformer(), "/v1/messages"},
		{"openai", transformers.NewOpenAITransformer(), "/v1/chat/completions"},
		{"gemini", transformers.NewGeminiTransformer(), "/v1beta/models"},
	}

	for _, p := range providersList {
		t.Run(p.name, func(t *testing.T) {
			endpoint := p.tr.Endpoint()
			if !strings.HasPrefix(endpoint, p.expectedPrefix) {
				t.Errorf("Provider %s endpoint %q should start with %q", p.name, endpoint, p.expectedPrefix)
			}
		})
	}
}

// TestStreamingEventTransformation tests streaming event transformation.
func TestStreamingEventTransformation(t *testing.T) {
	openaiTr := transformers.NewOpenAITransformer()

	// Test content delta event
	contentDelta := map[string]any{
		"id":      "chatcmpl-123",
		"object":  "chat.completion.chunk",
		"created": 1234567890,
		"model":   "gpt-4",
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"content": "Hello",
				},
			},
		},
	}

	deltaData, _ := json.Marshal(contentDelta)
	event := &transformer.SSEEvent{
		EventType: "chunk",
		Data:      deltaData,
	}

	transformed, err := openaiTr.TransformStreamEvent(event)
	if err != nil {
		t.Fatalf("TransformStreamEvent failed: %v", err)
	}

	if len(transformed) == 0 {
		t.Fatal("Expected at least one transformed event")
	}

	// Check for message_start
	hasMessageStart := false
	hasContentDelta := false
	for _, te := range transformed {
		if te.EventType == "message_start" {
			hasMessageStart = true
		}
		if te.EventType == "content_block_delta" {
			hasContentDelta = true
			var delta map[string]any
			if err := json.Unmarshal(te.Data, &delta); err == nil {
				if deltaMap, ok := delta["delta"].(map[string]any); ok {
					if text, ok := deltaMap["text"].(string); ok {
						if text != "Hello" {
							t.Errorf("Expected text 'Hello', got %s", text)
						}
					}
				}
			}
		}
	}

	if !hasMessageStart {
		t.Error("Expected message_start event")
	}
	if !hasContentDelta {
		t.Error("Expected content_block_delta event")
	}
}

// TestHTTPRoundTrip tests a full HTTP round trip with mock server.
func TestHTTPRoundTrip(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("Expected x-api-key header, got %v", r.Header.Get("x-api-key"))
		}

		// Send mock response
		resp := anthropic.Response{
			ID:         "msg_test",
			Type:       "message",
			Role:       anthropic.RoleAssistant,
			Model:      "claude-3-5-sonnet-20241022",
			StopReason: "end_turn",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: "Test response"},
			},
			Usage: anthropic.Usage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tr := transformers.NewAnthropicTransformer()
	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 100,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Test"},
				},
			},
		},
	}

	httpReq, err := tr.PrepareRequest(req, server.URL, "test-key", "claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	parsedResp, err := tr.ParseResponse(resp)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	if parsedResp.Content[0].Text != "Test response" {
		t.Errorf("Expected 'Test response', got %s", parsedResp.Content[0].Text)
	}
}
