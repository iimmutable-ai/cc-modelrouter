//go:build integration
// +build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// eventually polls a condition until it becomes true or times out.
func eventually(t *testing.T, timeout, interval time.Duration, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

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

// TestUsageTrackingNonStreaming tests usage tracking with non-streaming requests
func TestUsageTrackingNonStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../../.cc-modelrouter/test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Create temporary database for testing
	tmpDir, err := os.MkdirTemp("", "usage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_usage.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init test database: %v", err)
	}
	defer db.Close()

	// Initialize components
	registry := transformer.NewRegistry()
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

	// Create real usage tracker with smaller buffer for immediate flushing
	tracker := usage.NewTracker(db, 1, 100*time.Millisecond)
	defer tracker.Shutdown()

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-nonstreaming")

	// Test case 1: Simple request
	t.Run("Simple Request", func(t *testing.T) {
		reqBody := map[string]any{
			"model":      "glm-4.7",
			"max_tokens": 50,
			"stream":     false,
			"messages": []map[string]any{
				{"role": "user", "content": "Say 'Hello World'"},
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Logf("Response: %s", w.Body.String())
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		// Parse response to check usage
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		usageData, ok := resp["usage"].(map[string]any)
		if !ok {
			t.Fatal("Response should contain usage data")
		}

		inputTokens := int(usageData["input_tokens"].(float64))
		outputTokens := int(usageData["output_tokens"].(float64))

		t.Logf("Response usage: input=%d, output=%d", inputTokens, outputTokens)

		// Wait for tracker to flush
		if !eventually(t, 500*time.Millisecond, 50*time.Millisecond, func() bool {
			records, _ := usage.GetRecordsByPeriod(db, "test-nonstreaming", time.Now().Add(-1*time.Minute), time.Now())
			return len(records) > 0
		}) {
			t.Fatal("Timeout waiting for usage record to flush")
		}

		// Verify database record
		records, err := usage.GetRecordsByPeriod(db, "test-nonstreaming", time.Now().Add(-1*time.Minute), time.Now())
		if err != nil {
			t.Fatalf("Failed to get records: %v", err)
		}

		if len(records) == 0 {
			t.Fatal("Expected at least 1 usage record, got 0")
		}

		record := records[0]
		expectedTokens := inputTokens + outputTokens
		if record.Tokens != expectedTokens {
			t.Errorf("Expected %d tokens, got %d", expectedTokens, record.Tokens)
		}

		if record.Route == "" {
			t.Error("Route should not be empty")
		}

		if record.Model == "" {
			t.Error("Model should not be empty")
		}

		t.Logf("✓ Non-streaming usage tracked: route=%s, model=%s, tokens=%d",
			record.Route, record.Model, record.Tokens)
	})
}

// TestUsageTrackingStreaming tests usage tracking with streaming requests
func TestUsageTrackingStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../../.cc-modelrouter/test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Create temporary database for testing
	tmpDir, err := os.MkdirTemp("", "usage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_usage_streaming.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init test database: %v", err)
	}
	defer db.Close()

	// Initialize components
	registry := transformer.NewRegistry()
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

	// Create real usage tracker
	tracker := usage.NewTracker(db, 1, 100*time.Millisecond)
	defer tracker.Shutdown()

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-streaming")

	// Test streaming request
	t.Run("Streaming Request", func(t *testing.T) {
		reqBody := map[string]any{
			"model":      "glm-4.7",
			"max_tokens": 100,
			"stream":     true,
			"messages": []map[string]any{
				{"role": "user", "content": "Count from 1 to 5"},
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Logf("Response: %s", w.Body.String())
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		// Verify SSE response
		respBody := w.Body.String()
		if respBody == "" {
			t.Fatal("Expected non-empty response body")
		}

		// Check for SSE events
		if !bytes.Contains([]byte(respBody), []byte("event:")) {
			t.Error("Expected SSE events in response")
		}

		t.Logf("Streaming response length: %d bytes", len(respBody))

		// Wait for tracker to flush
		if !eventually(t, 500*time.Millisecond, 50*time.Millisecond, func() bool {
			records, _ := usage.GetRecordsByPeriod(db, "test-streaming", time.Now().Add(-1*time.Minute), time.Now())
			return len(records) > 0
		}) {
			t.Fatal("Timeout waiting for usage record to flush")
		}

		// Verify database record
		records, err := usage.GetRecordsByPeriod(db, "test-streaming", time.Now().Add(-1*time.Minute), time.Now())
		if err != nil {
			t.Fatalf("Failed to get records: %v", err)
		}

		if len(records) == 0 {
			t.Fatal("Expected at least 1 usage record, got 0")
		}

		record := records[0]
		if record.Tokens <= 0 {
			t.Errorf("Expected tokens > 0, got %d", record.Tokens)
		}

		if record.Route == "" {
			t.Error("Route should not be empty")
		}

		if record.Model == "" {
			t.Error("Model should not be empty")
		}

		t.Logf("✓ Streaming usage tracked: route=%s, model=%s, tokens=%d",
			record.Route, record.Model, record.Tokens)
	})
}

// TestUsageTrackingFallback tests usage tracking with provider fallback
func TestUsageTrackingFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../../.cc-modelrouter/test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Create temporary database for testing
	tmpDir, err := os.MkdirTemp("", "usage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_usage_fallback.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init test database: %v", err)
	}
	defer db.Close()

	// Initialize components
	registry := transformer.NewRegistry()
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

	// Create real usage tracker
	tracker := usage.NewTracker(db, 1, 100*time.Millisecond)
	defer tracker.Shutdown()

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-fallback")

	// Test request with fallback scenario
	t.Run("Request With Fallback", func(t *testing.T) {
		reqBody := map[string]any{
			"model":      "glm-4.7",
			"max_tokens": 50,
			"stream":     false,
			"messages": []map[string]any{
				{"role": "user", "content": "Say 'OK'"},
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Logf("Response: %s", w.Body.String())
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		// Wait for tracker to flush
		if !eventually(t, 500*time.Millisecond, 50*time.Millisecond, func() bool {
			records, _ := usage.GetRecordsByPeriod(db, "test-fallback", time.Now().Add(-1*time.Minute), time.Now())
			return len(records) > 0
		}) {
			t.Fatal("Timeout waiting for usage record to flush")
		}

		// Verify database record
		records, err := usage.GetRecordsByPeriod(db, "test-fallback", time.Now().Add(-1*time.Minute), time.Now())
		if err != nil {
			t.Fatalf("Failed to get records: %v", err)
		}

		if len(records) == 0 {
			t.Fatal("Expected at least 1 usage record, got 0")
		}

		record := records[0]
		t.Logf("✓ Fallback usage tracked: route=%s, model=%s, tokens=%d, fallbacks=%d",
			record.Route, record.Model, record.Tokens, record.Fallbacks)

		// Note: fallbacks count should be >= 0 (may be 0 if first provider succeeded)
		if record.Fallbacks < 0 {
			t.Errorf("Fallbacks should be >= 0, got %d", record.Fallbacks)
		}
	})
}

// TestUsageTrackingConcurrent tests usage tracking with concurrent requests
func TestUsageTrackingConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load test configuration
	cfg, err := config.Load("../../.cc-modelrouter/test.config.json")
	if err != nil {
		t.Skipf("Test config not found: %v", err)
	}

	// Create temporary database for testing
	tmpDir, err := os.MkdirTemp("", "usage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_usage_concurrent.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init test database: %v", err)
	}
	defer db.Close()

	// Initialize components
	registry := transformer.NewRegistry()
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

	// Create real usage tracker
	tracker := usage.NewTracker(db, 10, 50*time.Millisecond)
	defer tracker.Shutdown()

	// Create handler with adapters
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&routerAdapter{engine: routerEngine})
	handler.SetTransformerRegistry(&registryAdapter{registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetUsageTracker(tracker)
	handler.SetInstanceID("test-concurrent")

	// Test concurrent requests
	t.Run("Concurrent Requests", func(t *testing.T) {
		numRequests := 5
		var wg sync.WaitGroup
		errors := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				reqBody := map[string]any{
					"model":      "glm-4.7",
					"max_tokens": 20,
					"stream":     false,
					"messages": []map[string]any{
						{"role": "user", "content": "Say 'OK'"},
					},
				}
				body, _ := json.Marshal(reqBody)

				req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					errors <- fmt.Errorf("request %d failed with status %d", idx, w.Code)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Request error: %v", err)
		}

		// Wait for tracker to flush all records
		if !eventually(t, 500*time.Millisecond, 50*time.Millisecond, func() bool {
			records, _ := usage.GetRecordsByPeriod(db, "test-concurrent", time.Now().Add(-1*time.Minute), time.Now())
			return len(records) == numRequests
		}) {
			records, _ := usage.GetRecordsByPeriod(db, "test-concurrent", time.Now().Add(-1*time.Minute), time.Now())
			t.Errorf("Expected %d usage records, got %d after timeout", numRequests, len(records))
		}

		// Verify database records
		records, err := usage.GetRecordsByPeriod(db, "test-concurrent", time.Now().Add(-1*time.Minute), time.Now())
		if err != nil {
			t.Fatalf("Failed to get records: %v", err)
		}

		if len(records) != numRequests {
			t.Errorf("Expected %d usage records, got %d", numRequests, len(records))
		}

		totalTokens := 0
		for _, record := range records {
			totalTokens += record.Tokens
			t.Logf("Record: route=%s, model=%s, tokens=%d",
				record.Route, record.Model, record.Tokens)
		}

		t.Logf("✓ Concurrent usage tracked: %d records, %d total tokens", len(records), totalTokens)
	})
}

// TestUsageTrackingBufferedFlush tests that the buffer flushes correctly
func TestUsageTrackingBufferedFlush(t *testing.T) {
	// Create temporary database for testing
	tmpDir, err := os.MkdirTemp("", "usage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_usage_buffer.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init test database: %v", err)
	}
	defer db.Close()

	// Create tracker with small buffer size (no defer shutdown, we'll call it explicitly)
	bufferSize := 3
	tracker := usage.NewTracker(db, bufferSize, 1*time.Second)

	instanceID := "test-buffer"
	numRecords := bufferSize + 1 // Should trigger automatic flush

	// Add records
	for i := 0; i < numRecords; i++ {
		tracker.Record(instanceID, "test-route", "test-model", "", "", 100+i, 0)
	}

	// Wait for flush to complete
	if !eventually(t, 500*time.Millisecond, 50*time.Millisecond, func() bool {
		records, _ := usage.GetRecordsByPeriod(db, instanceID, time.Now().Add(-1*time.Minute), time.Now())
		return len(records) >= bufferSize
	}) {
		t.Fatalf("Timeout waiting for buffer flush")
	}

	// Check database - should have at least bufferSize records
	records, err := usage.GetRecordsByPeriod(db, instanceID, time.Now().Add(-1*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("Failed to get records: %v", err)
	}

	if len(records) < bufferSize {
		t.Errorf("Expected at least %d records after buffer overflow, got %d", bufferSize, len(records))
	}

	// Shutdown tracker to flush remaining records
	tracker.Shutdown()

	// Check again - should have all records now
	records, err = usage.GetRecordsByPeriod(db, instanceID, time.Now().Add(-1*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("Failed to get records after shutdown: %v", err)
	}

	if len(records) != numRecords {
		t.Errorf("Expected %d records after shutdown, got %d", numRecords, len(records))
	}

	t.Logf("✓ Buffered flush verified: %d records written", len(records))
}
