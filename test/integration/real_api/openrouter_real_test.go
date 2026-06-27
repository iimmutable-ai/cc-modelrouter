//go:build integration_real
// +build integration_real

package real_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// OpenRouter models to test
// Note: OpenRouter's Anthropic-compatible endpoint (https://openrouter.ai/api) only supports Anthropic models.
// For Google/Gemini models, use the direct Gemini provider or OpenRouter's OpenAI-compatible endpoint.
var openrouterModels = []string{"anthropic/claude-haiku-4.5", "anthropic/claude-sonnet-4.5"}

// TestOpenRouterSimpleCompletion tests simple text completion with OpenRouter.
func TestOpenRouterSimpleCompletion(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-simple")

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello'"},
		},
	}

	runRequestTest(t, handler, reqBody, "OpenRouter simple completion")
}

// TestOpenRouterStreaming tests streaming with OpenRouter.
func TestOpenRouterStreaming(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-streaming")

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"stream":     true,
		"max_tokens": 50,
		"messages": []map[string]any{
			{"role": "user", "content": "Count from 1 to 3"},
		},
	}

	runStreamingTest(t, handler, reqBody, "OpenRouter streaming")
}

// TestOpenRouterToolCalls tests tool calling with OpenRouter.
func TestOpenRouterToolCalls(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-tools")

	reqBody := map[string]any{
		"model": "anthropic/claude-haiku-4.5",
		"max_tokens": 200,
		"messages": []map[string]any{
			{"role": "user", "content": "What's 2+2? Use a calculator tool."},
		},
		"tools": []map[string]any{
			{
				"name":        "calculator",
				"description": "Perform mathematical calculations",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expression": map[string]any{
							"type":        "string",
							"description": "The mathematical expression to evaluate",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
	}

	runToolCallTest(t, handler, reqBody, "OpenRouter tool call")
}

// TestOpenRouterMultipleModels tests multiple model options with OpenRouter.
func TestOpenRouterMultipleModels(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	for _, model := range openrouterModels {
		t.Run(model, func(t *testing.T) {
			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
				WithRoute("default", "openrouter:"+model).
				Build()

			handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-"+model)

			reqBody := map[string]any{
				"model":      model,
				"max_tokens": 50,
				"messages": []map[string]any{
					{"role": "user", "content": "Say 'Hi'"},
				},
			}

			runRequestTest(t, handler, reqBody, "OpenRouter "+model)
		})
	}
}

// TestOpenRouterConcurrentRequests tests concurrent requests to OpenRouter.
func TestOpenRouterConcurrentRequests(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-concurrent")

	results := make(chan int, 5)

	// Make 5 concurrent requests
	for i := 0; i < 5; i++ {
		go func(id int) {
			reqBody := map[string]any{
				"model":      "anthropic/claude-haiku-4.5",
				"max_tokens": 20,
				"messages": []map[string]any{
					{"role": "user", "content": fmt.Sprintf("Request %d: Say 'OK'", id)},
				},
			}

			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				results <- 1
			} else {
				results <- 0
			}
		}(i)
	}

	// Collect results
	successCount := 0
	for i := 0; i < 5; i++ {
		successCount += <-results
	}

	if successCount != 5 {
		t.Errorf("Expected 5 successful concurrent requests, got %d", successCount)
	}

	t.Logf("OpenRouter concurrent requests test: %d/5 succeeded", successCount)
}

// TestOpenRouterContextCancellation tests context cancellation with OpenRouter.
func TestOpenRouterContextCancellation(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-cancellation")

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"max_tokens": 1000,
		"messages": []map[string]any{
			{"role": "user", "content": "Write a very long response..."},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("OpenRouter context cancellation test completed with status: %d", w.Code)
}

// TestOpenRouterMaxTokens tests max_tokens enforcement.
func TestOpenRouterMaxTokens(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-maxtokens")

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"max_tokens": 10,
		"messages": []map[string]any{
			{"role": "user", "content": "Write a very long essay about everything"},
		},
	}

	runRequestTest(t, handler, reqBody, "OpenRouter max_tokens")
}

// TestOpenRouterRateLimitHandling tests rate limit handling.
func TestOpenRouterRateLimitHandling(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	apiKey := getAPIKey("openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, apiKey, openrouterModels, "openrouter").
		WithRoute("default", "openrouter:anthropic/claude-haiku-4.5").
		WithRetryConfig(3, "200ms").
		Build()

	handler := createTestHandler(t, cfg, apiKey, OpenRouterBaseURL, "test-openrouter-ratelimit")

	// Make several rapid requests
	for i := 0; i < 5; i++ {
		reqBody := map[string]any{
			"model":      "anthropic/claude-haiku-4.5",
			"max_tokens": 20,
			"messages": []map[string]any{
				{"role": "user", "content": "Say 'OK'"},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		t.Logf("Request %d: status=%d", i+1, w.Code)
	}
}

// TestOpenRouterAuthenticationFailure tests authentication failure handling.
func TestOpenRouterAuthenticationFailure(t *testing.T) {
	skipIfNoKey(t, "openrouter")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", OpenRouterBaseURL, "invalid-api-key", openrouterModels, "openrouter").
		WithRoute("default", "openrouter:google/gemini-2.5-flash").
		Build()

	handler := createTestHandler(t, cfg, "invalid-api-key", OpenRouterBaseURL, "test-openrouter-auth-fail")

	reqBody := map[string]any{
		"model":      "google/gemini-2.5-flash",
		"max_tokens": 20,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}

	runRequestTest(t, handler, reqBody, "OpenRouter authentication failure")
}