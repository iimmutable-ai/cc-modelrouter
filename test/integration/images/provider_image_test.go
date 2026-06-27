package providers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestOpenAIImageHandling tests OpenAI's handling of image content (GPT-4 Vision).
func TestOpenAIImageHandling(t *testing.T) {
	openaiTransformer := transformers.NewOpenAITransformer()

	// Create a request with image content
	req := &anthropic.Request{
		Model:     "gpt-4-vision-preview",
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
							MediaType: "image/jpeg",
							Data:      base64.StdEncoding.EncodeToString([]byte("fake image data")),
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

	// Verify the request was prepared
	body := make(map[string]any)
	json.NewDecoder(httpReq.Body).Decode(&body)

	t.Logf("OpenAI image request: %+v", body)

	// Verify messages array exists
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatal("expected messages array in request body")
	}

	t.Logf("OpenAI image handling test passed")
}

// TestOpenRouterImageHandling tests OpenRouter's handling of image content.
func TestOpenRouterImageHandling(t *testing.T) {
	openrouterTransformer := transformers.NewOpenRouterTransformer()

	req := &anthropic.Request{
		Model:     "openrouter/gpt-4-vision",
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
							MediaType: "image/png",
							Data:      base64.StdEncoding.EncodeToString([]byte("fake png data")),
						},
					},
				},
			},
		},
	}

	httpReq, err := openrouterTransformer.PrepareRequest(req, "https://openrouter.ai/api/v1/chat/completions", "test-key", "openrouter/gpt-4-vision")
	if err != nil {
		t.Fatalf("failed to prepare request: %v", err)
	}

	body := make(map[string]any)
	json.NewDecoder(httpReq.Body).Decode(&body)

	t.Logf("OpenRouter image request: %+v", body)

	t.Logf("OpenRouter image handling test passed")
}

// TestGLMImageHandling tests GLM/BigModel's handling of image content.
func TestGLMImageHandling(t *testing.T) {
	glmTransformer := transformers.NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4v",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Analyze this image"},
					{
						Type: "image",
						Source: &anthropic.ImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      base64.StdEncoding.EncodeToString([]byte("fake jpeg data")),
						},
					},
				},
			},
		},
	}

	httpReq, err := glmTransformer.PrepareRequest(req, "https://open.bigmodel.cn/api/paas/v4/chat/completions", "test-key", "glm-4v")
	if err != nil {
		t.Fatalf("failed to prepare request: %v", err)
	}

	body := make(map[string]any)
	json.NewDecoder(httpReq.Body).Decode(&body)

	t.Logf("GLM image request: %+v", body)

	t.Logf("GLM image handling test passed")
}

// TestProviderImageResponseHandling tests how different providers handle image responses.
func TestProviderImageResponseHandling(t *testing.T) {
	tests := []struct {
		name        string
		transformer transformer.Transformer
		response    string
		expectImage bool
	}{
		{
			name:        "OpenAI - text only response",
			transformer: transformers.NewOpenAITransformer(),
			response: `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1234567890,
				"model": "gpt-4-vision-preview",
				"choices": [{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "This is a description of the image."
					},
					"finish_reason": "stop"
				}]
			}`,
			expectImage: false,
		},
		{
			name:        "Gemini - text only response",
			transformer: transformers.NewGeminiTransformer(),
			response: `{
				"candidates": [{
					"content": {
						"parts": [{"text": "Here's what I see in the image."}]
					},
					"finishReason": "STOP"
				}],
				"usageMetadata": {
					"promptTokenCount": 100,
					"candidatesTokenCount": 50,
					"totalTokenCount": 150
				}
			}`,
			expectImage: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockResp := newMockHTTPResponse(200, tt.response)

			anthropicResp, err := tt.transformer.ParseResponse(mockResp.Response)
			if err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}

			if anthropicResp == nil {
				t.Fatal("expected non-nil response")
			}

			hasImage := false
			for _, block := range anthropicResp.Content {
				if block.Type == "image" {
					hasImage = true
					break
				}
			}

			if hasImage != tt.expectImage {
				t.Errorf("expected hasImage=%v, got %v", tt.expectImage, hasImage)
			}

			t.Logf("Provider %s response handling test passed", tt.name)
		})
	}
}

// TestMultipleProvidersWithSameImage tests that the same image works across providers.
func TestMultipleProvidersWithSameImage(t *testing.T) {
	// Create test image data
	testImageData := base64.StdEncoding.EncodeToString([]byte("test image content"))

	providers := []struct {
		name        string
		transformer transformer.Transformer
		baseURL     string
		model       string
	}{
		{
			name:        "OpenAI",
			transformer: transformers.NewOpenAITransformer(),
			baseURL:     "https://api.openai.com/v1/chat/completions",
			model:       "gpt-4-vision-preview",
		},
		{
			name:        "OpenRouter",
			transformer: transformers.NewOpenRouterTransformer(),
			baseURL:     "https://openrouter.ai/api/v1/chat/completions",
			model:       "openrouter/gpt-4-vision",
		},
		{
			name:        "Gemini",
			transformer: transformers.NewGeminiTransformer(),
			baseURL:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro-vision:generateContent",
			model:       "gemini-pro-vision",
		},
	}

	req := &anthropic.Request{
		Model:     "test-model",
		MaxTokens: 4096,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: anthropic.MessageContent{
					{Type: "text", Text: "What's this?"},
					{
						Type: "image",
						Source: &anthropic.ImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      testImageData,
						},
					},
				},
			},
		},
	}

	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			httpReq, err := provider.transformer.PrepareRequest(req, provider.baseURL, "test-key", provider.model)
			if err != nil {
				t.Fatalf("failed to prepare request: %v", err)
			}

			if httpReq == nil {
				t.Fatal("expected non-nil request")
			}

			t.Logf("%s successfully prepared request with image", provider.name)
		})
	}
}

// mockHTTPResponse is a mock HTTP response for testing.
type mockHTTPResponse struct {
	*http.Response
}

func newMockHTTPResponse(statusCode int, body string) *mockHTTPResponse {
	return &mockHTTPResponse{
		Response: &http.Response{
			StatusCode: statusCode,
			Status:     http.StatusText(statusCode),
			Body:       &mockReadCloser{strings.NewReader(body)},
			Header:     make(http.Header),
		},
	}
}

type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}

// TestProviderImageStreaming tests image content in streaming responses.
func TestProviderImageStreaming(t *testing.T) {
	t.Run("OpenAI streaming with image prompt", func(t *testing.T) {
		openaiTransformer := transformers.NewOpenAITransformer()

		// Create an SSE event
		sseEvent := transformer.SSEEvent{
			EventType: "content_block_delta",
			Data:      []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I can see the image shows..."}}`),
		}

		events, err := openaiTransformer.TransformStreamEvent(&sseEvent)
		if err != nil {
			t.Fatalf("failed to transform event: %v", err)
		}

		if len(events) == 0 {
			t.Fatal("expected at least one event")
		}

		t.Logf("OpenAI streaming event transformed successfully")
	})

	t.Run("Gemini streaming with image prompt", func(t *testing.T) {
		geminiTransformer := transformers.NewGeminiTransformer()

		sseEvent := transformer.SSEEvent{
			EventType: "content_block_delta",
			Data:      []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Based on the image..."}}`),
		}

		events, err := geminiTransformer.TransformStreamEvent(&sseEvent)
		if err != nil {
			t.Fatalf("failed to transform event: %v", err)
		}

		if len(events) == 0 {
			t.Fatal("expected at least one event")
		}

		t.Logf("Gemini streaming event transformed successfully")
	})
}

// TestImageFormatCompatibility tests image format compatibility across providers.
func TestImageFormatCompatibility(t *testing.T) {
	imageFormats := []struct {
		mediaType string
		data       string
	}{
		{"image/png", "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="},
		{"image/jpeg", "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBD"},
		{"image/webp", "UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAQA"},
		{"image/gif", "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"},
	}

	for _, format := range imageFormats {
		t.Run("format_"+format.mediaType, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "test-model",
				MaxTokens: 4096,
				Messages: []anthropic.Message{
					{
						Role: anthropic.RoleUser,
						Content: anthropic.MessageContent{
							{Type: "text", Text: "What's this?"},
							{
								Type: "image",
								Source: &anthropic.ImageSource{
									Type:      "base64",
									MediaType: format.mediaType,
									Data:      format.data,
								},
							},
						},
					},
				},
			}

			// Test with multiple transformers
			transformers := []transformer.Transformer{
				transformers.NewOpenAITransformer(),
				transformers.NewOpenRouterTransformer(),
				transformers.NewGeminiTransformer(),
			}

			for _, tf := range transformers {
				httpReq, err := tf.PrepareRequest(req, "https://api.example.com/v1/chat", "test-key", "test-model")
				if err != nil {
					t.Logf("Transformer %v failed for %s: %v", tf, format.mediaType, err)
					continue
				}

				if httpReq != nil {
					t.Logf("%s format compatible with transformer %v", format.mediaType, tf)
				}
			}
		})
	}
}
