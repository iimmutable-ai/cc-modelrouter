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
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// failoverTracker tracks which provider was actually used
type failoverTracker struct {
	records []*failoverRecord
	mu      sync.Mutex
}

type failoverRecord struct {
	InstanceID string
	Route      string
	Model      string
	Tokens     int
	Fallbacks  int
}

func (m *failoverTracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, &failoverRecord{
		InstanceID: instanceID,
		Route:      route,
		Model:      model,
		Tokens:     tokens,
		Fallbacks:  fallbacks,
	})
}

func (m *failoverTracker) GetRecords() []*failoverRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records
}

// createFailoverTestHandler creates a handler configured for failover testing
func createFailoverTestHandler(t *testing.T, providers map[string]providerConfig, primaryModel string) *failoverTestContext {
	// Build route string for failover chain
	var routeParts []string
	for name := range providers {
		routeParts = append(routeParts, name+":"+primaryModel)
	}
	routeStr := ""
	for i, part := range routeParts {
		if i > 0 {
			routeStr += ";"
		}
		routeStr += part
	}

	// Build config using the exact pattern from working tests
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 18081,
			Host: "localhost",
		},
		Providers: make(map[string]config.ProviderConfig),
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": routeStr,
			},
			MaxRetries: 1,
			RetryDelay: "100ms",
		},
	}

	for name, pCfg := range providers {
		cfg.Providers[name] = config.ProviderConfig{
			BaseURL:     pCfg.baseURL,
			APIKey:      pCfg.apiKey,
			Models:      pCfg.models,
			Transformer: pCfg.transformer,
		}
	}

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())
	registry.Register(transformers.NewOpenAITransformer())
	registry.Register(transformers.NewGeminiTransformer())

	// Create clients for all providers
	clients := make(map[string]proxy.HTTPClient)
	for name, pCfg := range providers {
		client, err := provider.NewClient(&provider.ClientConfig{
			BaseURL:  pCfg.baseURL,
			APIKey:   pCfg.apiKey,
			Timeout:  "10s",
		})
		if err != nil {
			t.Fatalf("Failed to create client for %s: %v", name, err)
		}
		clients[name] = client
	}

	routerEngine := router.NewEngine(cfg)

	tracker := &failoverTracker{records: make([]*failoverRecord, 0)}

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-failover")

	return &failoverTestContext{
		handler: handler,
		tracker: tracker,
	}
}

type failoverTestContext struct {
	handler *proxy.Handler
	tracker *failoverTracker
}

type providerConfig struct {
	baseURL     string
	apiKey      string
	models      []string
	transformer string
}

// ========== BigModel to OpenRouter Failover Tests ==========

func TestRealFailover_BigModelInvalidKey_To_OpenRouter(t *testing.T) {
	openRouterKey := util.GetAPIKey("openrouter")
	if openRouterKey == "" {
		t.Skip("CCROUTER_OPENROUTER_API_KEY not set")
	}

	providers := map[string]providerConfig{
		"bigmodel": {
			baseURL:     BigmodelBaseURL,
			apiKey:      "INVALID_KEY_TO_FORCE_FAILOVER",
			models:      []string{"glm-4.7"},
			transformer: "anthropic",
		},
		"openrouter": {
			baseURL:     OpenRouterBaseURL,
			apiKey:      openRouterKey,
			models:      []string{"anthropic/claude-haiku-4.5"},
			transformer: "anthropic",
		},
	}

	// Use the first provider's primary model
	ctx := createFailoverTestHandler(t, providers, "anthropic/claude-haiku-4.5")

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"max_tokens": 50,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx.handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	if w.Code != http.StatusOK {
		t.Fatalf("Expected success after failover, got status %d: %s", w.Code, w.Body.String())
	}

	// Parse response
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)

	responseModel, _ := response["model"].(string)
	t.Logf("Response model after failover: %s", responseModel)

	// Check that failover occurred
	records := ctx.tracker.GetRecords()
	if len(records) == 0 {
		t.Fatal("No usage records found")
	}

	record := records[0]

	t.Logf("Failover test passed: fallbacks=%d, model=%s", record.Fallbacks, record.Model)

	// Verify the backup provider (OpenRouter) was used
	if record.Fallbacks == 0 {
		t.Error("Expected fallbacks > 0, but got 0 - failover may not have occurred")
	}
}

// ========== OpenRouter to Aliyun Failover Tests ==========

func TestRealFailover_OpenRouterInvalidKey_To_Aliyun(t *testing.T) {
	aliyunKey := util.GetAPIKey("aliyun")
	if aliyunKey == "" {
		t.Skip("CCROUTER_ALIYUN_API_KEY not set")
	}

	providers := map[string]providerConfig{
		"openrouter": {
			baseURL:     OpenRouterBaseURL,
			apiKey:      "INVALID_KEY_TO_FORCE_FAILOVER",
			models:      []string{"anthropic/claude-haiku-4.5"},
			transformer: "anthropic",
		},
		"aliyun": {
			baseURL:     AliyunBaseURL,
			apiKey:      aliyunKey,
			models:      []string{"glm-4.7"},
			transformer: "anthropic",
		},
	}

	ctx := createFailoverTestHandler(t, providers, "glm-4.7")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 50,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx.handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected success after failover, got status %d: %s", w.Code, w.Body.String())
	}

	records := ctx.tracker.GetRecords()
	if len(records) == 0 {
		t.Fatal("No usage records found")
	}

	record := records[0]

	t.Logf("Failover test passed: fallbacks=%d, model=%s", record.Fallbacks, record.Model)
}

// ========== Token Accuracy Tests Using Same Pattern ==========

func createAccuracyTestHandler(t *testing.T, providerName, apiKey, baseURL, model string) *accuracyTestContext {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 18081,
			Host: "localhost",
		},
		Providers: map[string]config.ProviderConfig{
			providerName: {
				BaseURL:     baseURL,
				APIKey:      apiKey,
				Models:      []string{model},
				Transformer: "anthropic",
			},
		},
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": providerName + ":" + model,
			},
		},
	}

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())
	registry.Register(transformers.NewOpenAITransformer())
	registry.Register(transformers.NewGeminiTransformer())

	client, err := provider.NewClient(&provider.ClientConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: "30s",
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	routerEngine := router.NewEngine(cfg)

	tracker := &accuracyTracker{records: make([]*accuracyRecord, 0)}

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{providerName: client})
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-accuracy")

	return &accuracyTestContext{
		handler: handler,
		tracker: tracker,
	}
}

type accuracyTestContext struct {
	handler *proxy.Handler
	tracker *accuracyTracker
}

type accuracyTracker struct {
	records []*accuracyRecord
	mu      sync.Mutex
}

type accuracyRecord struct {
	InstanceID string
	Route      string
	Model      string
	Tokens     int
}

func (m *accuracyTracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, &accuracyRecord{
		InstanceID: instanceID,
		Route:      route,
		Model:      model,
		Tokens:     tokens,
	})
}

func TestBigModelTokenAccuracy_Simple(t *testing.T) {
	apiKey := util.GetAPIKey("bigmodel")
	if apiKey == "" {
		t.Skip("CCROUTER_BIGMODEL_API_KEY not set")
	}

	ctx := createAccuracyTestHandler(t, "bigmodel", apiKey, BigmodelBaseURL, "glm-4.7")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 50,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx.handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Fatalf("Request failed with status %d: %s", w.Code, w.Body.String())
	}

	// Parse response to get provider-reported usage
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	usageData, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatal("No usage data in response")
	}

	providerInput := int(usageData["input_tokens"].(float64))
	providerOutput := int(usageData["output_tokens"].(float64))
	providerTotal := providerInput + providerOutput

	// Check tracker record
	ctx.tracker.mu.Lock()
	defer ctx.tracker.mu.Unlock()

	if len(ctx.tracker.records) == 0 {
		t.Fatal("No usage records found")
	}

	record := ctx.tracker.records[0]

	t.Logf("Provider reported: input=%d, output=%d, total=%d", providerInput, providerOutput, providerTotal)
	t.Logf("Tracked: tokens=%d, model=%s", record.Tokens, record.Model)

	if record.Tokens != providerTotal {
		t.Errorf("Token mismatch! Provider reported %d but tracker recorded %d", providerTotal, record.Tokens)
	} else {
		t.Logf("✓ Token tracking accurate!")
	}
}

func TestOpenRouterTokenAccuracy_Simple(t *testing.T) {
	apiKey := util.GetAPIKey("openrouter")
	if apiKey == "" {
		t.Skip("CCROUTER_OPENROUTER_API_KEY not set")
	}

	ctx := createAccuracyTestHandler(t, "openrouter", apiKey, OpenRouterBaseURL, "anthropic/claude-haiku-4.5")

	reqBody := map[string]any{
		"model":      "anthropic/claude-haiku-4.5",
		"max_tokens": 50,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx.handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Fatalf("Request failed with status %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)

	usageData, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatal("No usage data in response")
	}

	providerInput := int(usageData["input_tokens"].(float64))
	providerOutput := int(usageData["output_tokens"].(float64))
	providerTotal := providerInput + providerOutput

	ctx.tracker.mu.Lock()
	defer ctx.tracker.mu.Unlock()

	if len(ctx.tracker.records) == 0 {
		t.Fatal("No usage records found")
	}

	record := ctx.tracker.records[0]

	t.Logf("Provider reported: %d tokens, Tracked: %d tokens", providerTotal, record.Tokens)

	if record.Tokens != providerTotal {
		t.Errorf("Token mismatch! %d vs %d", providerTotal, record.Tokens)
	} else {
		t.Logf("✓ Token tracking accurate!")
	}
}
