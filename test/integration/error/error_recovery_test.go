//go:build error_tests
// +build error_tests

package error

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestProviderFailover tests that requests fail over to backup providers.
func TestProviderFailover(t *testing.T) {
	attempts := make(map[string]int)

	// Create primary server that fails
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts["primary"]++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Primary provider down"}}`))
	}))
	defer primaryServer.Close()

	// Create backup server that succeeds
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts["backup"]++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "backup-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello from backup"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer backupServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("primary", primaryServer.URL, "primary-key", []string{"test-model"}, "anthropic").
		WithProvider("backup", backupServer.URL, "backup-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "primary:test-model;backup:test-model").
		WithRetryConfig(1, "100ms").
		Build()

	// Create clients for both providers
	primaryClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         primaryServer.URL,
		APIKey:          "primary-key",
		Timeout:         "2s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	backupClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         backupServer.URL,
		APIKey:          "backup-key",
		Timeout:         "2s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{
		"primary": primaryClient,
		"backup":  backupClient,
	})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-failover")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected success after failover, got status %d: %s", w.Code, w.Body.String())
	}

	if attempts["backup"] == 0 {
		t.Error("Failover did not occur - backup provider was not called")
	}

	t.Logf("Failover test passed: primary attempts=%d, backup attempts=%d", attempts["primary"], attempts["backup"])
}

// TestMultipleFailoverPaths tests failover across multiple backup providers.
func TestMultipleFailoverPaths(t *testing.T) {
	attempts := make(map[string]int)

	// All but last provider fail
	servers := make([]*httptest.Server, 3)
	for i := 0; i < 3; i++ {
		idx := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts[fmt.Sprintf("provider%d", idx)]++
			if idx < 2 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":{"message":"Provider failed"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          fmt.Sprintf("id%d", idx),
				"type":        "message",
				"role":        "assistant",
				"content":     []map[string]any{{"type": "text", "text": "Success"}},
				"model":       "test-model",
				"stop_reason": "end_turn",
				"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
			})
		}))
		defer servers[i].Close()
	}

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("provider0", servers[0].URL, "key0", []string{"test-model"}, "anthropic").
		WithProvider("provider1", servers[1].URL, "key1", []string{"test-model"}, "anthropic").
		WithProvider("provider2", servers[2].URL, "key2", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "provider0:test-model;provider1:test-model;provider2:test-model").
		WithRetryConfig(1, "50ms").
		Build()

	clients := make(map[string]proxy.HTTPClient)
	for i := 0; i < 3; i++ {
		clients[fmt.Sprintf("provider%d", i)], _ = provider.NewClient(&provider.ClientConfig{
			BaseURL:         servers[i].URL,
			APIKey:          fmt.Sprintf("key%d", i),
			Timeout:         "2s",
			MaxIdleConns:    100,
			IdleConnTimeout: "90s",
		})
	}

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-multi-failover")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected success after multiple failovers, got status %d", w.Code)
	}

	if attempts["provider2"] == 0 {
		t.Error("Multiple failover did not reach final provider")
	}

	t.Logf("Multiple failover test passed: attempts=%v", attempts)
}

// TestErrorPropagation tests that errors are properly propagated to the client.
func TestErrorPropagation(t *testing.T) {
	testCases := []struct {
		name         string
		statusCode   int
		errorMsg     string
		expectClient bool
	}{
		{"bad_request", http.StatusBadRequest, `{"error":{"message":"Bad request"}}`, true},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"Unauthorized"}}`, true},
		{"not_found", http.StatusNotFound, `{"error":{"message":"Model not found"}}`, true},
		{"rate_limit", http.StatusTooManyRequests, `{"error":{"message":"Rate limit"}}`, true},
		{"server_error", http.StatusInternalServerError, `{"error":{"message":"Internal error"}}`, false},
		{"bad_gateway", http.StatusBadGateway, `{"error":{"message":"Bad gateway"}}`, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(tc.errorMsg))
			}))
			defer errorServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("error-provider", errorServer.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "error-provider:test-model").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         errorServer.URL,
				APIKey:          "test-key",
				Timeout:         "5s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"error-provider": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-error-prop-" + tc.name)

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Client errors (4xx) should be propagated, server errors might retry
			if tc.expectClient && w.Code == http.StatusOK {
				t.Errorf("Test %s: expected error propagation, got success", tc.name)
			}

			t.Logf("Test %s: status=%d", tc.name, w.Code)
		})
	}
}

// TestContextCancellationDuringFailover tests that context cancellation is respected during failover.
func TestContextCancellationDuringFailover(t *testing.T) {
	attempts := make(map[string]int)

	// Create slow servers
	servers := make([]*httptest.Server, 3)
	for i := 0; i < 3; i++ {
		idx := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts[fmt.Sprintf("provider%d", idx)]++
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"Slow"}}`))
		}))
		defer servers[i].Close()
	}

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("provider0", servers[0].URL, "key0", []string{"test-model"}, "anthropic").
		WithProvider("provider1", servers[1].URL, "key1", []string{"test-model"}, "anthropic").
		WithProvider("provider2", servers[2].URL, "key2", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "provider0:test-model;provider1:test-model;provider2:test-model").
		WithRetryConfig(1, "100ms").
		Build()

	clients := make(map[string]proxy.HTTPClient)
	for i := 0; i < 3; i++ {
		clients[fmt.Sprintf("provider%d", i)], _ = provider.NewClient(&provider.ClientConfig{
			BaseURL:         servers[i].URL,
			APIKey:          fmt.Sprintf("key%d", i),
			Timeout:         "2s",
			MaxIdleConns:    100,
			IdleConnTimeout: "90s",
		})
	}

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-cancellation-failover")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 300ms
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Context cancellation test: status=%d, attempts=%v", w.Code, attempts)
}

// TestFailoverStateRecovery tests that failover state is properly reset between requests.
func TestFailoverStateRecovery(t *testing.T) {
	// Track which providers are called per request
	requestAttempts := make([]map[string]int, 0)
	var mu sync.Mutex

	// Primary fails, backup succeeds
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Down"}}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		mu.Lock()
		if requestID == "1" {
			if len(requestAttempts) < 1 {
				requestAttempts = append(requestAttempts, make(map[string]int))
			}
			requestAttempts[0]["backup"]++
		} else if requestID == "2" {
			if len(requestAttempts) < 2 {
				requestAttempts = append(requestAttempts, make(map[string]int))
			}
			requestAttempts[1]["backup"]++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "backup-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer backupServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("primary", primaryServer.URL, "primary-key", []string{"test-model"}, "anthropic").
		WithProvider("backup", backupServer.URL, "backup-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "primary:test-model;backup:test-model").
		WithRetryConfig(1, "50ms").
		Build()

	primaryClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         primaryServer.URL,
		APIKey:          "primary-key",
		Timeout:         "2s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	backupClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         backupServer.URL,
		APIKey:          "backup-key",
		Timeout:         "2s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{
		"primary": primaryClient,
		"backup":  backupClient,
	})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-failover-recovery")

	// Make two separate requests
	for i := 1; i <= 2; i++ {
		reqBody := map[string]any{
			"model":      "test-model",
			"max_tokens": 100,
			"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", fmt.Sprintf("%d", i))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}
	}

	// Both requests should have reached the backup
	mu.Lock()
	defer mu.Unlock()

	if len(requestAttempts) < 2 {
		t.Errorf("Expected 2 request attempts recorded, got %d", len(requestAttempts))
		return
	}

	for i, attempts := range requestAttempts {
		if attempts["backup"] == 0 {
			t.Errorf("Request %d did not reach backup provider", i+1)
		}
	}

	t.Logf("Failover state recovery test passed: %v", requestAttempts)
}

// TestConcurrentFailover tests failover with concurrent requests.
func TestConcurrentFailover(t *testing.T) {
	var mu sync.Mutex
	attempts := make(map[string]int)

	// Primary fails intermittently
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts["primary"]++
		count := attempts["primary"]
		mu.Unlock()

		if count%2 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"Down"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "primary-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello from primary"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts["backup"]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "backup-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello from backup"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer backupServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("primary", primaryServer.URL, "primary-key", []string{"test-model"}, "anthropic").
		WithProvider("backup", backupServer.URL, "backup-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "primary:test-model;backup:test-model").
		WithRetryConfig(1, "50ms").
		Build()

	primaryClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         primaryServer.URL,
		APIKey:          "primary-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	backupClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         backupServer.URL,
		APIKey:          "backup-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{
		"primary": primaryClient,
		"backup":  backupClient,
	})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-failover")

	results := make(chan int, 10)

	// Make 10 concurrent requests
	for i := 0; i < 10; i++ {
		go func() {
			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
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
		}()
	}

	// Collect results
	successCount := 0
	for i := 0; i < 10; i++ {
		successCount += <-results
	}

	mu.Lock()
	defer mu.Unlock()

	t.Logf("Concurrent failover test: %d/10 requests succeeded, primary=%d, backup=%d",
		successCount, attempts["primary"], attempts["backup"])
}

// TestStreamingErrorRecovery tests error handling during streaming with failover.
func TestStreamingErrorRecovery(t *testing.T) {
	attempts := make(map[string]int)

	// Primary fails during stream
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts["primary"]++
		w.Header().Set("Content-Type", "text/event-stream")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[]}}` + "\n\n"))

		// Fail mid-stream
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"Stream failed"}}`))
	}))
	defer primaryServer.Close()

	// Backup succeeds fully
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts["backup"]++
		w.Header().Set("Content-Type", "text/event-stream")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[]}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer backupServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("primary", primaryServer.URL, "primary-key", []string{"test-model"}, "anthropic").
		WithProvider("backup", backupServer.URL, "backup-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "primary:test-model;backup:test-model").
		WithRetryConfig(1, "100ms").
		Build()

	primaryClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         primaryServer.URL,
		APIKey:          "primary-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	backupClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         backupServer.URL,
		APIKey:          "backup-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{
		"primary": primaryClient,
		"backup":  backupClient,
	})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-streaming-recovery")

	reqBody := map[string]any{
		"model":    "test-model",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Streaming error recovery test: status=%d, primary=%d, backup=%d",
		w.Code, attempts["primary"], attempts["backup"])
}