//go:build integration_real
// +build integration_real

package real_api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// Provider base URLs - consistent across all tests
const (
	AliyunBaseURL     = "https://coding.dashscope.aliyuncs.com/apps/anthropic"
	BigmodelBaseURL   = "https://open.bigmodel.cn/api/anthropic"
	OpenRouterBaseURL = "https://openrouter.ai/api"
)

// mockTracker implements usage.Tracker for testing
type mockTracker struct {
	records []*usage.Record
	mu      sync.Mutex
}

func (m *mockTracker) Record(instanceID, route, model string, tokens, fallbacks int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, &usage.Record{
		InstanceID: instanceID,
		Route:      route,
		Model:      model,
		Tokens:     tokens,
	})
}

func (m *mockTracker) GetRecords() []*usage.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records
}

// Load config from file or create test config
func loadTestConfig(t *testing.T, providerName, apiKey, baseURL string, models []string, transformer string) (*config.Config, error) {
	// Try to load from file first, fall back to in-memory config
	cfg, err := config.Load("../../.cc-modelrouter/" + providerName + "-test.config.json")
	if err != nil {
		// Create in-memory config
		cfg = &config.Config{
			Server: config.ServerConfig{
				Port: 18081,
				Host: "localhost",
			},
			Providers: map[string]config.ProviderConfig{
				providerName: {
					APIKey:      apiKey,
					BaseURL:     baseURL,
					Transformer: transformer,
					Models:      models,
				},
			},
			Router: config.RouterConfig{
				Routes: map[string]string{
					"default": providerName + ":" + models[0],
				},
			},
		}
	}
	return cfg, err
}

// ========================================
// HELPER FUNCTIONS
// ========================================

// createTestHandler creates a test handler with all necessary components
func createTestHandler(t *testing.T, cfg *config.Config, apiKey, baseURL string, instanceID string) *proxy.Handler {
	registry := transformer.NewRegistry()

	// Register all new transformers
	registry.Register(transformers.NewAnthropicTransformer())
	registry.Register(transformers.NewOpenAITransformer())
	registry.Register(transformers.NewGeminiTransformer())
	registry.Register(transformers.NewOpenRouterTransformer())
	registry.Register(transformers.NewGLMAnthropicTransformer())
	// Note: MiniMax and Qwen use the Anthropic transformer since they provide
	// Anthropic-compatible APIs. OpenRouter and GLM have specialized transformers
	// for provider-specific signature handling.

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
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID(instanceID)

	return handler
}

// runRequestTest runs a non-streaming request test
func runRequestTest(t *testing.T, handler *proxy.Handler, reqBody map[string]any, testName string) {
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Running %s test...", testName)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("%s test: status=%d", testName, w.Code)

	if w.Code == http.StatusOK {
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
			if id, ok := resp["id"].(string); ok {
				t.Logf("%s test: Response ID=%s", testName, id)
			}
			if model, ok := resp["model"].(string); ok {
				t.Logf("%s test: Model=%s", testName, model)
			}
			if content, ok := resp["content"].([]any); ok && len(content) > 0 {
				for _, c := range content {
					if contentMap, ok := c.(map[string]any); ok {
						if textType, ok := contentMap["type"].(string); ok && textType == "text" {
							if text, ok := contentMap["text"].(string); ok {
								t.Logf("%s test: Content=%s", testName, text)
							}
						}
					}
				}
			}
		}
	} else {
		t.Logf("%s test failed: %s", testName, w.Body.String())
	}
}

// runStreamingTest runs a streaming request test
func runStreamingTest(t *testing.T, handler *proxy.Handler, reqBody map[string]any, testName string) {
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Running %s test...", testName)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("%s test: status=%d", testName, w.Code)
	contentType := w.Header().Get("Content-Type")
	t.Logf("%s test: Content-Type=%s", testName, contentType)

	if w.Code == http.StatusOK {
		responseBody := w.Body.String()
		t.Logf("%s test: Response length=%d bytes", testName, len(responseBody))
	} else {
		t.Logf("%s test failed: %s", testName, w.Body.String())
	}
}

// runToolCallTest runs a tool call test
func runToolCallTest(t *testing.T, handler *proxy.Handler, reqBody map[string]any, testName string) {
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Running %s test...", testName)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("%s test: status=%d", testName, w.Code)

	if w.Code == http.StatusOK {
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
			if content, ok := resp["content"].([]any); ok && len(content) > 0 {
				hasToolUse := false
				for _, c := range content {
					if contentMap, ok := c.(map[string]any); ok {
						if contentType, ok := contentMap["type"].(string); ok && contentType == "tool_use" {
							hasToolUse = true
							t.Logf("%s test: Tool use detected", testName)
						}
					}
				}
				if !hasToolUse {
					t.Logf("%s test: No tool use, model may have answered directly", testName)
				}
			}
		}
	} else {
		t.Logf("%s test failed: %s", testName, w.Body.String())
	}
}