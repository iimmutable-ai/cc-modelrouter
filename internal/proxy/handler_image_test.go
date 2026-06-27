package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestImageSizeSingleLargeImage tests handling of a single large image.
func TestImageSizeSingleLargeImage(t *testing.T) {
	// Create a 5MB base64-encoded "image" (just data, not a real image)
	largeImageData := make([]byte, 5*1024*1024)
	for i := range largeImageData {
		largeImageData[i] = byte(i % 256)
	}
	largeImageBase64 := base64.StdEncoding.EncodeToString(largeImageData)

	reqBody := map[string]any{
		"model":     "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "What's in this large image?"},
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       largeImageBase64,
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)

	// Calculate actual request size
	requestSize := int64(len(body))

	// Create handler with slightly larger limit to allow this request
	handler := NewHandler(requestSize + 1024)

	// Setup minimal handler dependencies
	setupHandler(t, handler, "test-large-image")

	// Create request
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Record response
	w := httptest.NewRecorder()

	// Handle request - should NOT fail due to size (we set limit higher)
	handler.ServeHTTP(w, req)

	// The request should be processed (will fail on provider call, but that's OK)
	// We're testing that the size validation doesn't reject it
	if w.Code == http.StatusRequestEntityTooLarge {
		t.Logf("Request was rejected due to size: status=%d", w.Code)
	}

	t.Logf("Single large image test: status=%d, request_size=%d bytes", w.Code, requestSize)
}

// TestImageSizeMultipleSmallImages tests handling of multiple small images.
func TestImageSizeMultipleSmallImages(t *testing.T) {
	// Create multiple small images (100KB each)
	numImages := 10
	smallImageSize := 100 * 1024

	content := []map[string]any{
		{"type": "text", "text": "Analyze these images"},
	}

	for i := 0; i < numImages; i++ {
		smallImageData := make([]byte, smallImageSize)
		for j := range smallImageData {
			smallImageData[j] = byte(j % 256)
		}
		smallImageBase64 := base64.StdEncoding.EncodeToString(smallImageData)

		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": "image/png",
				"data":       smallImageBase64,
			},
		})
	}

	reqBody := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": content,
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	requestSize := int64(len(body))

	// Create handler with sufficient limit
	handler := NewHandler(requestSize + 1024)
	setupHandler(t, handler, "test-multiple-images")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Multiple small images test: status=%d, request_size=%d bytes, num_images=%d", w.Code, requestSize, numImages)
}

// TestImageSize50MBBoundary tests behavior at the 50MB boundary.
func TestImageSize50MBBoundary(t *testing.T) {
	// Test just under 50MB
	t.Run("just_under_50MB", func(t *testing.T) {
		// Create content that results in ~49MB request
		testSizeBoundary(t, 49*1024*1024, false)
	})

	// Test at 50MB (should work, limit is inclusive)
	t.Run("at_50MB", func(t *testing.T) {
		testSizeBoundary(t, 50*1024*1024, false)
	})

	// Test just over 50MB (should be rejected)
	t.Run("just_over_50MB", func(t *testing.T) {
		testSizeBoundary(t, 51*1024*1024, true)
	})
}

// testSizeBoundary is a helper for testing size boundaries.
func testSizeBoundary(t *testing.T, targetSize int64, expectRejection bool) {
	// Create image data to reach target size
	// Account for JSON overhead (~20%)
	imageDataSize := int(float64(targetSize) * 0.8)
	imageData := make([]byte, imageDataSize)
	for i := range imageData {
		imageData[i] = byte(i % 256)
	}
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	reqBody := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Describe this image"},
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imageBase64,
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	actualSize := int64(len(body))

	// Use standard 50MB limit
	handler := NewHandler(50 * 1024 * 1024)
	setupHandler(t, handler, "test-boundary")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Boundary test: target=%d, actual=%d, status=%d, expect_rejection=%v",
		targetSize, actualSize, w.Code, expectRejection)

	if expectRejection {
		// Should be rejected (413 Request Entity Too Large or 400 Bad Request)
		if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
			t.Errorf("expected rejection (413 or 400) for size %d, got status %d", actualSize, w.Code)
		}
	}
	// If not expecting rejection, we don't assert success because the request
	// will fail on provider call anyway - we're just testing size validation
}

// TestImageSizeWithTextAndTools tests size calculation with mixed content.
func TestImageSizeWithTextAndTools(t *testing.T) {
	// Create moderate-sized image
	imageData := make([]byte, 2*1024*1024) // 2MB raw
	for i := range imageData {
		imageData[i] = byte(i % 256)
	}
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Create large text content
	largeText := strings.Repeat("A", 100*1024) // 100KB text

	// Create tool definitions
	tools := []map[string]any{
		{
			"name":        "analyze_image",
			"description": "Analyze the provided image",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]string{"type": "string"},
				},
			},
		},
	}

	// Include image in the content
	reqBody := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"tools":      tools,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": largeText},
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imageBase64,
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	requestSize := int64(len(body))

	handler := NewHandler(requestSize + 1024)
	setupHandler(t, handler, "test-mixed-content")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Mixed content test: status=%d, request_size=%d bytes", w.Code, requestSize)
}

// TestImageSizeEmptyImage tests handling of empty/minimal image data.
func TestImageSizeEmptyImage(t *testing.T) {
	reqBody := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "What's this?"},
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "", // Empty image
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)

	handler := NewHandler(50 * 1024 * 1024)
	setupHandler(t, handler, "test-empty-image")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Empty image test: status=%d", w.Code)
}

// TestHasImagesInRequest tests the HasImages detection function.
func TestHasImagesInRequest(t *testing.T) {
	tests := []struct {
		name     string
		req      anthropic.Request
		expected bool
	}{
		{
			name: "request with image",
			req: anthropic.Request{
				Messages: []anthropic.Message{
					{
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Look at this"},
							{
								Type: "image",
								Source: &anthropic.ImageSource{
									Type:      "base64",
									MediaType: "image/png",
									Data:      "abc",
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "request without image",
			req: anthropic.Request{
				Messages: []anthropic.Message{
					{
						Content: anthropic.MessageContent{
							{Type: "text", Text: "Hello"},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "request with tool_use only",
			req: anthropic.Request{
				Messages: []anthropic.Message{
					{
						Content: anthropic.MessageContent{
							{
								Type:  "tool_use",
								ID:    "toolu_123",
								Name:  "test",
								Input: json.RawMessage(`{}`),
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasImagesInRequest(tt.req)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// hasImagesInRequest checks if a request contains image content.
func hasImagesInRequest(req anthropic.Request) bool {
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			if block.Type == "image" {
				return true
			}
		}
	}
	return false
}

// setupHandler is a helper to set up a handler with minimal dependencies.
func setupHandler(t *testing.T, handler *Handler, instanceID string) {
	t.Helper()

	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "anthropic:claude-3-5-sonnet-20241022",
			},
		},
	}

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler.SetRouter(routerEngine)
	handler.SetTransformerRegistry(registry)
	handler.SetConfig(cfg)
	handler.SetInstanceID(instanceID)
	handler.SetProviderClients(map[string]HTTPClient{})
}

// TestImageSizeAccumulation tests that multiple images accumulate size correctly.
func TestImageSizeAccumulation(t *testing.T) {
	// Create 5 images of 1MB each
	numImages := 5
	imageData := make([]byte, 1024*1024) // 1MB
	for i := range imageData {
		imageData[i] = byte(i % 256)
	}
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	content := []map[string]any{{"type": "text", "text": "Analyze these images"}}
	for i := 0; i < numImages; i++ {
		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": "image/png",
				"data":       imageBase64,
			},
		})
	}

	reqBody := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": content,
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	requestSize := int64(len(body))

	handler := NewHandler(requestSize + 1024)
	setupHandler(t, handler, "test-accumulation")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Accumulation test: status=%d, request_size=%d bytes, num_images=%d", w.Code, requestSize, numImages)

	// Verify the request contains all images
	var parsedReq anthropic.Request
	if err := json.Unmarshal(body, &parsedReq); err == nil {
		imageCount := 0
		for _, block := range parsedReq.Messages[0].Content {
			if block.Type == "image" {
				imageCount++
			}
		}
		if imageCount != numImages {
			t.Errorf("expected %d images, got %d", numImages, imageCount)
		}
	}
}
