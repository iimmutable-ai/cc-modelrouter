//go:build integration
// +build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
)

// routerAdapter adapts router.Engine to proxy.Router interface
type routerAdapter struct {
	engine *router.Engine
}

func (a *routerAdapter) DetectRoute(req router.RouteRequest) string {
	return a.engine.DetectRoute(req)
}

func (a *routerAdapter) GetTargets(routeName string) []config.RouteTarget {
	return a.engine.GetTargets(routeName)
}

// registryAdapter adapts transformer.Registry to proxy.TransformerRegistry interface
type registryAdapter struct {
	registry *transformer.Registry
}

func (a *registryAdapter) Get(name string) (transformer.Transformer, error) {
	return a.registry.Get(name)
}

// mockTracker records to a slice for testing
type mockTracker struct {
	records []*usage.Record
	mu      sync.Mutex
}

func (m *mockTracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, &usage.Record{
		InstanceID: instanceID,
		Profile:    profile,
		Provider:   provider,
		Route:      route,
		Model:      model,
		Tokens:     tokens,
		Fallbacks:  fallbacks,
		Timestamp:  time.Now(),
	})
}

func (m *mockTracker) Shutdown() {
	// No-op for mock tracker
}

func TestIntegrationBasicRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../.cc-modelrouter/test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Initialize components
	registry := transformer.NewRegistry()
	// GLM uses Anthropic-compatible transformer
	registry.Register(transformers.NewAnthropicTransformer())

	clients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		client, _ := provider.NewClient(&provider.ClientConfig{
			BaseURL: providerCfg.BaseURL,
			APIKey:  providerCfg.APIKey,
		})
		clients[name] = client
	}

	routerEngine := router.NewEngine(cfg)

	// Create mock usage tracker for testing
	tracker := &mockTracker{records: make([]*usage.Record, 0)}

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-inst")

	// Create test request
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello'"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Logf("Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify usage was tracked
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if len(tracker.records) != 1 {
		t.Errorf("Expected 1 usage record, got %d", len(tracker.records))
		return
	}

	record := tracker.records[0]
	if record.Route == "" {
		t.Error("Route should not be empty")
	}
	if record.Model == "" {
		t.Error("Model should not be empty")
	}
	if record.Tokens <= 0 {
		t.Errorf("Tokens should be > 0, got %d", record.Tokens)
	}
	if record.InstanceID != "test-inst" {
		t.Errorf("InstanceID should be 'test-inst', got '%s'", record.InstanceID)
	}

	t.Logf("Usage tracking verified: route=%s, model=%s, tokens=%d",
		record.Route, record.Model, record.Tokens)
}
