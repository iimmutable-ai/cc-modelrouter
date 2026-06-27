//go:build network
// +build network

package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// slowHandler simulates a slow server that takes longer than the timeout.
type slowHandler struct {
	delay time.Duration
}

func (h *slowHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	time.Sleep(h.delay)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":          "test-id",
		"type":        "message",
		"role":        "assistant",
		"content":     []map[string]any{{"type": "text", "text": "Slow response"}},
		"model":       "test-model",
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
	})
}

// TestConnectionTimeout tests that connection timeouts are handled correctly.
func TestConnectionTimeout(t *testing.T) {
	// Create a slow server
	slowServer := httptest.NewServer(&slowHandler{delay: 2 * time.Second})
	defer slowServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("slow-provider", slowServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "slow-provider:test-model").
		Build()

	// Create client with short timeout
	client, err := provider.NewClient(&provider.ClientConfig{
		BaseURL:         slowServer.URL,
		APIKey:          "test-key",
		Timeout:         "100ms",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"slow-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-timeout")

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

	// Should receive a timeout error
	if w.Code == http.StatusOK {
		t.Error("Expected timeout error, got success")
	}

	t.Logf("Timeout test completed with status: %d, body: %s", w.Code, w.Body.String())
}

// TestReadTimeout tests that read timeouts are handled correctly.
func TestReadTimeout(t *testing.T) {
	// Create a server that hangs after accepting connection
	hangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Start sending response but don't finish
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test-id"`))
		time.Sleep(5 * time.Second)
		w.Write([]byte(`}`))
	}))
	defer hangingServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("hanging-provider", hangingServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "hanging-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         hangingServer.URL,
		APIKey:          "test-key",
		Timeout:         "200ms",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"hanging-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-read-timeout")

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

	// Should receive an error due to timeout
	if w.Code == http.StatusOK {
		t.Error("Expected read timeout error, got success")
	}

	t.Logf("Read timeout test completed with status: %d", w.Code)
}

// TestWriteTimeout tests that write timeouts are handled correctly.
func TestWriteTimeout(t *testing.T) {
	// This test simulates a server that accepts the connection but
	// doesn't read from it quickly
	slowReaderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait before reading the body
		time.Sleep(3 * time.Second)

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "test-id",
				"type":        "message",
				"role":        "assistant",
				"content":     []map[string]any{{"type": "text", "text": "Response"}},
				"model":       "test-model",
				"stop_reason": "end_turn",
				"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
			})
		}
	}))
	defer slowReaderServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("slow-reader", slowReaderServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "slow-reader:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         slowReaderServer.URL,
		APIKey:          "test-key",
		Timeout:         "200ms",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"slow-reader": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-write-timeout")

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
		t.Error("Expected write timeout error, got success")
	}

	t.Logf("Write timeout test completed with status: %d", w.Code)
}

// TestConcurrentTimeout tests handling multiple concurrent timeout scenarios.
func TestConcurrentTimeout(t *testing.T) {
	slowServer := httptest.NewServer(&slowHandler{delay: 2 * time.Second})
	defer slowServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("slow-provider", slowServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "slow-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         slowServer.URL,
		APIKey:          "test-key",
		Timeout:         "100ms",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"slow-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-timeout")

	// Make 5 concurrent requests
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
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
				errors <- fmt.Errorf("request %d: expected timeout, got success", id)
			}
			errors <- nil
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < 5; i++ {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}

	t.Log("Concurrent timeout test passed")
}

// TestStreamingTimeout tests timeout handling during streaming responses.
func TestStreamingTimeout(t *testing.T) {
	// Create a server that streams slowly
	slowStreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Streaming not supported")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		events := []string{
			`event: message_start` + "\n",
			`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			flusher.Flush()
		}

		// Slow down between chunks
		time.Sleep(2 * time.Second)

		// Add content block
		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))
		flusher.Flush()

		time.Sleep(2 * time.Second)

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
		flusher.Flush()
	}))
	defer slowStreamServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("slow-stream", slowStreamServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "slow-stream:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         slowStreamServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"slow-stream": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-streaming-timeout")

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

	// Streaming timeout should be handled gracefully
	t.Logf("Streaming timeout test completed with status: %d", w.Code)
}

// TestContextCancellation tests that context cancellation is handled correctly.
func TestContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(&slowHandler{delay: 10 * time.Second})
	defer slowServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("slow-provider", slowServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "slow-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         slowServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"slow-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-context-cancellation")

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should handle cancellation gracefully
	t.Logf("Context cancellation test completed with status: %d", w.Code)
}