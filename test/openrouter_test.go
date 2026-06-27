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
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
)

// TestOpenRouterClaudeSonnet45 tests a real API call to OpenRouter using Claude Sonnet 4.5
func TestOpenRouterClaudeSonnet45(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../.cc-modelrouter/openrouter-test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Initialize components
	registry := transformer.NewRegistry()
	// OpenRouter uses Anthropic-compatible API
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
	handler.SetInstanceID("test-inst-openrouter")

	// Create test request using Anthropic API format
	// The proxy expects Anthropic format and uses the transformer to convert to OpenRouter
	reqBody := map[string]any{
		"model":      "anthropic/claude-sonnet-4.5",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' and tell me your name"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	t.Log("Sending request to OpenRouter/Claude Sonnet 4.5...")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Log response
	t.Logf("Response Status: %d", w.Code)
	t.Logf("Response Body: %s", w.Body.String())

	// Check response
	if w.Code != http.StatusOK {
		t.Logf("Full Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify response body contains expected content
	responseStr := w.Body.String()
	if len(responseStr) == 0 {
		t.Error("Response body is empty")
	}

	// Verify the response is valid JSON
	var jsonResponse map[string]any
	if err := json.Unmarshal([]byte(responseStr), &jsonResponse); err != nil {
		t.Logf("Response: %s", responseStr)
		t.Errorf("Response is not valid JSON: %v", err)
	} else {
		t.Logf("Valid JSON response received")

		// Check for typical OpenRouter response fields
		if id, ok := jsonResponse["id"].(string); ok {
			t.Logf("Response ID: %s", id)
		}
		if model, ok := jsonResponse["model"].(string); ok {
			t.Logf("Model: %s", model)
		}
		if choices, ok := jsonResponse["choices"].([]any); ok && len(choices) > 0 {
			t.Logf("Choices count: %d", len(choices))
		}
	}

	// Verify usage was tracked
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if len(tracker.records) != 1 {
		t.Errorf("Expected 1 usage record, got %d", len(tracker.records))
		return
	}

	record := tracker.records[0]
	t.Logf("Usage tracking verified: route=%s, model=%s, tokens=%d",
		record.Route, record.Model, record.Tokens)

	if record.Route == "" {
		t.Error("Route should not be empty")
	}
	if record.Model == "" {
		t.Error("Model should not be empty")
	}
	if record.Tokens <= 0 {
		t.Errorf("Tokens should be > 0, got %d", record.Tokens)
	}
	if record.InstanceID != "test-inst-openrouter" {
		t.Errorf("InstanceID should be 'test-inst-openrouter', got '%s'", record.InstanceID)
	}
}

// TestOpenRouterClaudeSonnet45Streaming tests streaming with OpenRouter and Claude Sonnet 4.5
func TestOpenRouterClaudeSonnet45Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../.cc-modelrouter/openrouter-test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Initialize components
	registry := transformer.NewRegistry()
	// OpenRouter uses Anthropic-compatible API
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

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(cli.NewRouterAdapter(routerEngine))
	handler.SetTransformerRegistry(cli.NewRegistryAdapter(registry))
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-inst-openrouter-streaming")

	// Create streaming request
	reqBody := map[string]any{
		"model":      "anthropic/claude-sonnet-4.5",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	t.Log("Sending streaming request to OpenRouter/Claude Sonnet 4.5...")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Log response status
	t.Logf("Response Status: %d", w.Code)

	// Check response
	if w.Code != http.StatusOK {
		t.Logf("Full Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify SSE headers
	contentType := w.Header().Get("Content-Type")
	t.Logf("Content-Type: %s", contentType)

	// Parse the SSE response
	responseBody := w.Body.String()
	t.Logf("SSE Response:\n%s", responseBody)

	// Parse SSE events
	events := parseSSEEvents(t, responseBody)

	if len(events) == 0 {
		t.Error("No SSE events received")
	}

	t.Logf("Received %d SSE events", len(events))

	// Check for expected event types
	eventTypes := make(map[string]bool)
	for _, event := range events {
		eventTypes[event.eventType] = true
	}

	t.Logf("Event types received: %v", eventTypes)

	// Verify we have at least one event with data
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

	// Check for content in the stream
	for _, event := range events {
		dataStr := string(event.data)
		if len(dataStr) > 0 {
			t.Logf("Event %s data: %s", event.eventType, dataStr)
		}
	}

	// Verify the response contains expected text (e.g., "Hello")
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

// containsWord checks if the response contains a given word (case-insensitive)
func containsWord(data, word string) bool {
	dataLower := toLower(data)
	wordLower := toLower(word)
	return contains(dataLower, wordLower)
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func contains(s, substr string) bool {
	return indexString(s, substr) >= 0
}

func indexString(s, substr string) int {
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}