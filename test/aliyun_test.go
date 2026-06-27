//go:build integration
// +build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/cli"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
)

// TestAliyunGLM47 tests a real API call to Aliyun DashScope using GLM-4.7
func TestAliyunGLM47(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := loadAliyunConfig()
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	handler := createAliyunTestHandler(t, cfg, "test-inst-aliyun-glm47")

	// Create test request for GLM-4.7
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' and tell me your name"},
		},
	}

	runAliyunTest(t, handler, reqBody, "Aliyun/GLM-4.7", "test-inst-aliyun-glm47")
}

// TestAliyunGLM47Streaming tests streaming with Aliyun and GLM-4.7
func TestAliyunGLM47Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := loadAliyunConfig()
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	handler := createAliyunStreamingTestHandler(t, cfg, "test-inst-aliyun-glm47-stream")

	// Create streaming request for GLM-4.7
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	runAliyunStreamingTest(t, handler, reqBody, "Aliyun/GLM-4.7", "test-inst-aliyun-glm47-stream")
}

// TestAliyunMiniMaxM25 tests a real API call to Aliyun DashScope using MiniMax-M2.5
func TestAliyunMiniMaxM25(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := loadAliyunConfig()
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	handler := createAliyunTestHandler(t, cfg, "test-inst-aliyun-minimax")

	// Create test request for MiniMax-M2.5
	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' and tell me your name"},
		},
	}

	runAliyunTest(t, handler, reqBody, "Aliyun/MiniMax-M2.5", "test-inst-aliyun-minimax")
}

// TestAliyunMiniMaxM25Streaming tests streaming with Aliyun and MiniMax-M2.5
func TestAliyunMiniMaxM25Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := loadAliyunConfig()
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	handler := createAliyunStreamingTestHandler(t, cfg, "test-inst-aliyun-minimax-stream")

	// Create streaming request for MiniMax-M2.5 (correct model name with dot)
	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	runAliyunStreamingTest(t, handler, reqBody, "Aliyun/MiniMax-M2.5", "test-inst-aliyun-minimax-stream")
}

// Helper functions

func loadAliyunConfig() (*config.Config, error) {
	return config.Load("../.cc-modelrouter/aliyun-test.config.json")
}

func createAliyunTestHandler(t *testing.T, cfg *config.Config, instanceID string) *proxy.Handler {
	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer()) // GLM uses Anthropic-compatible transformer
	registry.Register(transformers.NewAnthropicTransformer()) // MiniMax uses Anthropic-compatible transformer

	clients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		client, err := provider.NewClient(&provider.ClientConfig{
			BaseURL:  providerCfg.BaseURL,
			APIKey:   providerCfg.APIKey,
			Timeout:  "60s",
		})
		if err != nil {
			t.Logf("Failed to create client for %s: %v", name, err)
		} else {
			clients[name] = client
			t.Logf("Created client for %s with baseURL: %s", name, providerCfg.BaseURL)
		}
	}

	routerEngine := router.NewEngine(cfg)
	tracker := &mockTracker{records: make([]*usage.Record, 0)}

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID(instanceID)

	return handler
}

func createAliyunStreamingTestHandler(t *testing.T, cfg *config.Config, instanceID string) *proxy.Handler {
	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer()) // GLM uses Anthropic-compatible transformer
	registry.Register(transformers.NewAnthropicTransformer()) // MiniMax uses Anthropic-compatible transformer

	clients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		client, err := provider.NewClient(&provider.ClientConfig{
			BaseURL:  providerCfg.BaseURL,
			APIKey:   providerCfg.APIKey,
			Timeout:  "60s",
		})
		if err != nil {
			t.Logf("Failed to create client for %s: %v", name, err)
		} else {
			clients[name] = client
			t.Logf("Created client for %s with baseURL: %s", name, providerCfg.BaseURL)
		}
	}

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(cli.NewRouterAdapter(routerEngine))
	handler.SetTransformerRegistry(cli.NewRegistryAdapter(registry))
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetInstanceID(instanceID)

	return handler
}

func runAliyunTest(t *testing.T, handler *proxy.Handler, reqBody map[string]any, testName, instanceID string) {
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Sending request to %s...", testName)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response Status: %d", w.Code)
	t.Logf("Response Body: %s", w.Body.String())

	if w.Code != http.StatusOK {
		t.Logf("Full Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	responseStr := w.Body.String()
	if len(responseStr) == 0 {
		t.Error("Response body is empty")
	}

	var jsonResponse map[string]any
	if err := json.Unmarshal([]byte(responseStr), &jsonResponse); err != nil {
		t.Logf("Response: %s", responseStr)
		t.Errorf("Response is not valid JSON: %v", err)
	} else {
		t.Logf("Valid JSON response received")
		if id, ok := jsonResponse["id"].(string); ok {
			t.Logf("Response ID: %s", id)
		}
		if model, ok := jsonResponse["model"].(string); ok {
			t.Logf("Model: %s", model)
		}
		if role, ok := jsonResponse["role"].(string); ok {
			t.Logf("Role: %s", role)
		}
		if content, ok := jsonResponse["content"].([]any); ok && len(content) > 0 {
			t.Logf("Content blocks: %d", len(content))
			for i, block := range content {
				if blockMap, ok := block.(map[string]any); ok {
					if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
						if text, ok := blockMap["text"].(string); ok {
							t.Logf("Content block %d: %s", i, text)
						}
					}
				}
			}
		}
	}
}

func runAliyunStreamingTest(t *testing.T, handler *proxy.Handler, reqBody map[string]any, testName, instanceID string) {
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Sending streaming request to %s...", testName)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response Status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Logf("Full Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	t.Logf("Content-Type: %s", contentType)

	responseBody := w.Body.String()
	t.Logf("SSE Response:\n%s", responseBody)

	events := parseSSEEvents(t, responseBody)

	if len(events) == 0 {
		t.Error("No SSE events received")
	}

	t.Logf("Received %d SSE events", len(events))

	eventTypes := make(map[string]bool)
	for _, event := range events {
		eventTypes[event.eventType] = true
	}

	t.Logf("Event types received: %v", eventTypes)

	hasData := false
	for _, event := range events {
		if len(event.data) > 0 {
			hasData = true
			break
		}
	}

	if !hasData {
		t.Error("No SSE events contain data")
	}

	for _, event := range events {
		dataStr := string(event.data)
		if len(dataStr) > 0 {
			t.Logf("Event %s data: %s", event.eventType, dataStr)
		}
	}

	responseContainsHello := false
	for _, event := range events {
		dataStr := string(event.data)
		if containsWord(dataStr, "Hello") {
			responseContainsHello = true
			break
		}
	}

	if !responseContainsHello {
		t.Logf("Warning: Response does not appear to contain 'Hello'. Full response logged above.")
	}
}