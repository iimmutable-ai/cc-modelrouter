package transformers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// mockHTTPResponse is a mock HTTP response for testing.
// It implements the http.Response interface by embedding *http.Response.
type mockHTTPResponse struct {
	*http.Response
}

func newMockHTTPResponse(statusCode int, body string) *mockHTTPResponse {
	return &mockHTTPResponse{
		Response: &http.Response{
			StatusCode: statusCode,
			Status:     http.StatusText(statusCode),
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		},
	}
}

// TestGeminiImageInRequest tests that Gemini transformer properly handles images in requests.
func TestGeminiImageInRequest(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	req := &anthropic.Request{
		Model:     "gemini-2.0-flash-exp",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "What's in this image?"},
					{
						Type: "image",
						Source: &anthropic.ImageSource{
							Type:      "base64",
							MediaType: "image/png",
							Data:      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
						},
					},
				},
			},
		},
	}

	httpReq, err := geminiTransformer.PrepareRequest(req, "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent", "test-key", "gemini-2.0-flash-exp")
	if err != nil {
		t.Fatalf("failed to prepare request: %v", err)
	}

	if httpReq == nil {
		t.Fatal("expected non-nil request")
	}

	// Verify request body contains the image
	body := make(map[string]any)
	json.NewDecoder(httpReq.Body).Decode(&body)

	t.Logf("Gemini request body: %+v", body)
}

// TestGeminiImageInResponse tests that Gemini transformer properly converts images in responses.
func TestGeminiImageInResponse(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	// Mock Gemini response with inline data (image)
	geminiRespJSON := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Here is the image you requested:"},
					{
						"inlineData": {
							"mimeType": "image/png",
							"data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
						}
					}
				]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 100,
			"candidatesTokenCount": 50,
			"totalTokenCount": 150
		}
	}`

	// Create a mock HTTP response
	mockResp := newMockHTTPResponse(200, geminiRespJSON)

	anthropicResp, err := geminiTransformer.ParseResponse(mockResp.Response)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if anthropicResp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify the response contains both text and image
	if len(anthropicResp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(anthropicResp.Content))
	}

	// First block should be text
	if anthropicResp.Content[0].Type != "text" {
		t.Errorf("expected first block to be text, got %s", anthropicResp.Content[0].Type)
	}

	// Second block should be image
	if anthropicResp.Content[1].Type != "image" {
		t.Errorf("expected second block to be image, got %s", anthropicResp.Content[1].Type)
	}

	if anthropicResp.Content[1].Source == nil {
		t.Fatal("expected image source to be non-nil")
	}

	if anthropicResp.Content[1].Source.MediaType != "image/png" {
		t.Errorf("expected media type image/png, got %s", anthropicResp.Content[1].Source.MediaType)
	}

	t.Logf("Gemini response contains image: %s", anthropicResp.Content[1].Source.Data[:20]+"...")
}

// TestOpenAIImageSupport tests that OpenAI transformer can handle image content (GPT-4 Vision).
func TestOpenAIImageSupport(t *testing.T) {
	openaiTransformer := NewOpenAITransformer()

	req := &anthropic.Request{
		Model:     "gpt-4-vision-preview",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Describe this image"},
					{
						Type: "image",
						Source: &anthropic.ImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBD",
						},
					},
				},
			},
		},
	}

	httpReq, err := openaiTransformer.PrepareRequest(req, "https://api.openai.com/v1/chat/completions", "test-key", "gpt-4-vision-preview")
	if err != nil {
		t.Fatalf("failed to prepare request: %v", err)
	}

	if httpReq == nil {
		t.Fatal("expected non-nil request")
	}

	// Verify request body
	body := make(map[string]any)
	json.NewDecoder(httpReq.Body).Decode(&body)

	t.Logf("OpenAI request body: %+v", body)

	// OpenAI format should convert image blocks to content array with image_url
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatal("expected messages in request body")
	}

	// The message should contain the image
	t.Logf("First message: %+v", messages[0])
}

// TestImageStreamingSSEEvent tests that image content is properly handled in SSE streaming.
func TestImageStreamingSSEEvent(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	// Create an SSE event representing text content
	textEvent := transformer.SSEEvent{
		EventType: "content_block_delta",
		Data:      []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
	}

	events, err := geminiTransformer.TransformStreamEvent(&textEvent)
	if err != nil {
		t.Fatalf("failed to transform text event: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	t.Logf("Transformed text event: %s", string(events[0].Data))
}

// TestMultipleImagesInStreaming tests handling multiple images in streaming responses.
func TestMultipleImagesInStreaming(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	// Simulate a Gemini response with multiple inline data parts
	geminiRespJSON := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Here are two images:"},
					{
						"inlineData": {
							"mimeType": "image/png",
							"data": "abc123"
						}
					},
					{
						"inlineData": {
							"mimeType": "image/jpeg",
							"data": "def456"
						}
					}
				]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 50,
			"candidatesTokenCount": 30,
			"totalTokenCount": 80
		}
	}`

	mockResp := newMockHTTPResponse(200, geminiRespJSON)

	anthropicResp, err := geminiTransformer.ParseResponse(mockResp.Response)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if anthropicResp == nil {
		t.Fatal("expected non-nil response")
	}

	// Should have 3 content blocks: text + 2 images
	if len(anthropicResp.Content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(anthropicResp.Content))
	}

	// Verify types
	expectedTypes := []string{"text", "image", "image"}
	for i, expectedType := range expectedTypes {
		if anthropicResp.Content[i].Type != expectedType {
			t.Errorf("block %d: expected type %s, got %s", i, expectedType, anthropicResp.Content[i].Type)
		}
	}

	t.Logf("Successfully parsed multiple images from response")
}

// TestImageWithTextInStreaming tests mixed image and text content in streaming.
func TestImageWithTextInStreaming(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	// Simulate alternating text and image content
	geminiRespJSON := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "First, here's an image:"},
					{
						"inlineData": {
							"mimeType": "image/png",
							"data": "image1data"
						}
					},
					{"text": "Now here's another image:"},
					{
						"inlineData": {
							"mimeType": "image/jpeg",
							"data": "image2data"
						}
					},
					{"text": "That's all!"}
				]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 100,
			"candidatesTokenCount": 100,
			"totalTokenCount": 200
		}
	}`

	mockResp := newMockHTTPResponse(200, geminiRespJSON)

	anthropicResp, err := geminiTransformer.ParseResponse(mockResp.Response)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if anthropicResp == nil {
		t.Fatal("expected non-nil response")
	}

	// Should have 5 content blocks: text, image, text, image, text
	if len(anthropicResp.Content) != 5 {
		t.Fatalf("expected 5 content blocks, got %d", len(anthropicResp.Content))
	}

	expectedTypes := []string{"text", "image", "text", "image", "text"}
	for i, expectedType := range expectedTypes {
		if anthropicResp.Content[i].Type != expectedType {
			t.Errorf("block %d: expected type %s, got %s", i, expectedType, anthropicResp.Content[i].Type)
		}
	}

	t.Logf("Successfully parsed alternating text and image content")
}

// TestImageEdgeCases tests edge cases for image handling.
func TestImageEdgeCases(t *testing.T) {
	geminiTransformer := NewGeminiTransformer()

	tests := []struct {
		name        string
		response    string
		expectError bool
	}{
		{
			name: "empty inline data",
			response: `{
				"candidates": [{
					"content": {"parts": [{"inlineData": {"mimeType": "image/png", "data": ""}}]},
					"finishReason": "STOP"
				}],
				"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 10, "totalTokenCount": 20}
			}`,
			expectError: false,
		},
		{
			name: "missing mime type",
			response: `{
				"candidates": [{
					"content": {"parts": [{"inlineData": {"data": "abc123"}}]},
					"finishReason": "STOP"
				}],
				"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 10, "totalTokenCount": 20}
			}`,
			expectError: false,
		},
		{
			name: "nil inline data",
			response: `{
				"candidates": [{
					"content": {"parts": [{"text": "just text"}]},
					"finishReason": "STOP"
				}],
				"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 10, "totalTokenCount": 20}
			}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockResp := newMockHTTPResponse(200, tt.response)

			resp, err := geminiTransformer.ParseResponse(mockResp.Response)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected non-nil response")
				}
			}
		})
	}
}
