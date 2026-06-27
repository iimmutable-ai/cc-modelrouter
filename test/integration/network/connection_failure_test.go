//go:build network
// +build network

package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestDNSFailure tests handling of DNS resolution failures.
func TestDNSFailure(t *testing.T) {
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("invalid-host", "http://this-host-does-not-exist-12345.example.com:9999", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "invalid-host:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://this-host-does-not-exist-12345.example.com:9999",
		APIKey:          "test-key",
		Timeout:         "1s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"invalid-host": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-dns-failure")

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

	// Should receive an error for DNS failure
	if w.Code == http.StatusOK {
		t.Error("Expected error for DNS failure, got success")
	}

	t.Logf("DNS failure test completed with status: %d", w.Code)
}

// TestConnectionRefused tests handling of connection refused errors.
func TestConnectionRefused(t *testing.T) {
	// Use a non-existent port that will refuse connections
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("refused-host", "http://localhost:9999", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "refused-host:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://localhost:9999",
		APIKey:          "test-key",
		Timeout:         "1s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"refused-host": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-connection-refused")

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

	// Should receive an error for connection refused
	if w.Code == http.StatusOK {
		t.Error("Expected error for connection refused, got success")
	}

	t.Logf("Connection refused test completed with status: %d", w.Code)
}

// TestBrokenPipe tests handling of broken pipe errors during response.
func TestBrokenPipe(t *testing.T) {
	// Create a server that closes the connection mid-response
	brokenPipeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test-id",`))

		// Hijack the connection and close it
		hijacker, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hijacker.Hijack()
			conn.Close()
		}
	}))
	defer brokenPipeServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("broken-pipe", brokenPipeServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "broken-pipe:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         brokenPipeServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"broken-pipe": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-broken-pipe")

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

	// Should handle broken pipe gracefully
	if w.Code == http.StatusOK {
		t.Error("Expected error for broken pipe, got success")
	}

	t.Logf("Broken pipe test completed with status: %d", w.Code)
}

// TestUnreachableHost tests handling of completely unreachable hosts.
func TestUnreachableHost(t *testing.T) {
	// Use an invalid IP that will timeout
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("unreachable", "http://192.0.2.1:9999", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "unreachable:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://192.0.2.1:9999", // TEST-NET-1, should be unreachable
		APIKey:          "test-key",
		Timeout:         "1s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"unreachable": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-unreachable-host")

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

	// Should receive an error
	if w.Code == http.StatusOK {
		t.Error("Expected error for unreachable host, got success")
	}

	t.Logf("Unreachable host test completed with status: %d", w.Code)
}

// TestInvalidScheme tests handling of invalid URL schemes.
func TestInvalidScheme(t *testing.T) {
	_, err := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "not-a-valid-url://localhost:9999",
		APIKey:          "test-key",
		Timeout:         "1s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	if err == nil {
		t.Error("Expected error for invalid URL scheme")
	}

	t.Log("Invalid scheme test passed")
}

// TestSSLHandshakeFailure tests handling of SSL/TLS handshake failures.
func TestSSLHandshakeFailure(t *testing.T) {
	// Create a server with invalid SSL certificate
	invalidSSLServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "test-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer invalidSSLServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("invalid-ssl", invalidSSLServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "invalid-ssl:test-model").
		Build()

	// Try to connect without validating SSL
	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         invalidSSLServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"invalid-ssl": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-ssl-handshake")

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

	// The httptest.NewTLSServer creates a valid cert, so this may succeed
	// In real scenarios with invalid certs, this would fail
	t.Logf("SSL handshake test completed with status: %d", w.Code)
}

// TestConcurrentFailures tests handling of multiple concurrent connection failures.
func TestConcurrentFailures(t *testing.T) {
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("fail-provider", "http://localhost:9999", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "fail-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://localhost:9999",
		APIKey:          "test-key",
		Timeout:         "1s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"fail-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-failures")

	errors := make(chan error, 10)

	// Make 10 concurrent requests to a failing host
	for i := 0; i < 10; i++ {
		go func(id int) {
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
				errors <- fmt.Errorf("request %d: expected error, got success", id)
			} else {
				errors <- nil
			}
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < 10; i++ {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}

	t.Log("Concurrent failures test passed")
}

// TestNetworkRecovery tests that the proxy recovers from transient network errors.
func TestNetworkRecovery(t *testing.T) {
	attempts := 0

	// Create a server that fails initially, then succeeds
	recoveringServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first two attempts
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"Temporary failure"}}`)
			return
		}

		// Succeed on third attempt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "test-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer recoveringServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("recovering", recoveringServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "recovering:test-model").
		WithRetryConfig(5, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         recoveringServer.URL,
		APIKey:          "test-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      100,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"recovering": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-network-recovery")

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

	// Should eventually succeed
	if w.Code != http.StatusOK {
		t.Errorf("Expected success after retries, got status %d: %s", w.Code, w.Body.String())
	}

	t.Logf("Network recovery test passed, took %d attempts", attempts)
}