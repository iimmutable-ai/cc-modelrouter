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
	"sync"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// Bigmodel models to test
var bigmodelModels = []string{"glm-4.7", "glm-4.6v", "glm-4.5-air"}

// TestGLMSimpleCompletion tests simple text completion with BigModel (GLM).
func TestGLMSimpleCompletion(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-simple")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' and tell me your name"},
		},
	}

	runRequestTest(t, handler, reqBody, "Bigmodel GLM simple completion")
}

// TestGLMStreaming tests streaming with BigModel (GLM).
func TestGLMStreaming(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-streaming")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	runStreamingTest(t, handler, reqBody, "Bigmodel GLM streaming")
}

// TestGLMToolCalls tests tool calling with BigModel (GLM).
func TestGLMToolCalls(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-tools")

	reqBody := map[string]any{
		"model": "glm-4.7",
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

	runToolCallTest(t, handler, reqBody, "Bigmodel GLM tool call")
}

// TestGLMMultipleModels tests multiple model options with BigModel.
func TestGLMMultipleModels(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	for _, model := range bigmodelModels {
		t.Run(model, func(t *testing.T) {
			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
				WithRoute("default", "bigmodel:"+model).
				Build()

			handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-"+model)

			reqBody := map[string]any{
				"model":      model,
				"max_tokens": 100,
				"messages": []map[string]any{
					{"role": "user", "content": "Say 'Hi'"},
				},
			}

			runRequestTest(t, handler, reqBody, "Bigmodel "+model)
		})
	}
}

// TestGLMConcurrentRequests tests concurrent requests to BigModel.
func TestGLMConcurrentRequests(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.5-air").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-concurrent")

	results := make(chan int, 5)

	// Make 5 concurrent requests
	for i := 0; i < 5; i++ {
		go func(id int) {
			reqBody := map[string]any{
				"model":      "glm-4.5-air",
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

	t.Logf("Bigmodel concurrent requests test: %d/5 succeeded", successCount)
}

// TestGLMContextCancellation tests context cancellation with BigModel.
func TestGLMContextCancellation(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-cancellation")

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 1000,
		"messages": []map[string]any{
			{"role": "user", "content": "Write a very long article..."},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Bigmodel context cancellation test completed with status: %d", w.Code)
}

// TestGLMMaxTokens tests max_tokens enforcement.
func TestGLMMaxTokens(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.5-air").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-maxtokens")

	reqBody := map[string]any{
		"model":      "glm-4.5-air",
		"max_tokens": 10,
		"messages": []map[string]any{
			{"role": "user", "content": "Write a very long article"},
		},
	}

	runRequestTest(t, handler, reqBody, "Bigmodel max_tokens")
}

// TestGLMChineseLanguage tests Chinese language support.
func TestGLMChineseLanguage(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-chinese")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "请用中文介绍一下你自己"},
		},
	}

	runRequestTest(t, handler, reqBody, "Bigmodel Chinese language")
}

// TestGLMAuthenticationFailure tests authentication failure handling.
func TestGLMAuthenticationFailure(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, "invalid-api-key", bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.5-air").
		Build()

	handler := createTestHandler(t, cfg, "invalid-api-key", BigmodelBaseURL, "test-glm-auth-fail")

	reqBody := map[string]any{
		"model":      "glm-4.5-air",
		"max_tokens": 20,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}

	runRequestTest(t, handler, reqBody, "Bigmodel authentication failure")
}

// ========================================
// HELPER FUNCTIONS
// ========================================

// TestStats tracks test statistics
type TestStats struct {
	mu            sync.Mutex
	totalTests    int
	passedTests   int
	failedTests   int
	skippedTests  int
	testDurations map[string]time.Duration
}

var globalStats = &TestStats{
	testDurations: make(map[string]time.Duration),
}

// PrintStats prints all test statistics
func PrintStats(t *testing.T) {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	t.Logf("=== Test Statistics ===")
	t.Logf("Total: %d | Passed: %d | Failed: %d | Skipped: %d",
		globalStats.totalTests,
		globalStats.passedTests,
		globalStats.failedTests,
		globalStats.skippedTests,
	)
	for testName, duration := range globalStats.testDurations {
		t.Logf("%s: %v", testName, duration)
	}
}

// AddTestResult records a test result
func AddTestResult(testName string, passed bool, duration time.Duration) {
	globalStats.mu.Lock()
	defer globalStats.mu.Unlock()

	globalStats.totalTests++
	globalStats.testDurations[testName] = duration
	if passed {
		globalStats.passedTests++
	} else {
		globalStats.failedTests++
	}
}