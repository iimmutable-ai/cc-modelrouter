//go:build load
// +build load

package load

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

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestConcurrentSimpleRequests tests multiple concurrent simple requests.
func TestConcurrentSimpleRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-simple")

	concurrency := 50
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", id)}},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent simple requests: %d/%d succeeded in %v", successCount, concurrency, duration)

	if successCount != concurrency {
		t.Errorf("Expected all %d requests to succeed, got %d", concurrency, successCount)
	}
}

// TestConcurrentStreamingRequests tests multiple concurrent streaming requests.
func TestConcurrentStreamingRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[]}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-streaming")

	concurrency := 20
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":    "test-model",
				"stream":   true,
				"max_tokens": 30,
				"messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", id)}},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent streaming requests: %d/%d succeeded in %v", successCount, concurrency, duration)

	if successCount != concurrency {
		t.Errorf("Expected all %d streaming requests to succeed, got %d", concurrency, successCount)
	}
}

// TestConcurrentMixedRequests tests concurrent requests with different types (streaming and non-streaming).
func TestConcurrentMixedRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "true" {
			w.Header().Set("Content-Type", "text/event-stream")

			w.Write([]byte(`event: message_start` + "\n"))
			w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[]}}` + "\n\n"))

			w.Write([]byte(`event: content_block_delta` + "\n"))
			w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Stream"}}` + "\n\n"))

			w.Write([]byte(`event: message_stop` + "\n"))
			w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
				"type":        "message",
				"role":        "assistant",
				"content":     []map[string]any{{"type": "text", "text": "Non-stream response"}},
				"model":       "test-model",
				"stop_reason": "end_turn",
				"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
			})
		}
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-mixed")

	totalRequests := 30
	results := make(chan int, totalRequests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Alternate between streaming and non-streaming
			isStreaming := id%2 == 0

			reqBody := map[string]any{
				"model":      "test-model",
				"stream":     isStreaming,
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", id)}},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent mixed requests: %d/%d succeeded in %v", successCount, totalRequests, duration)

	if successCount != totalRequests {
		t.Errorf("Expected all %d requests to succeed, got %d", totalRequests, successCount)
	}
}

// TestConcurrentRequestsDifferentModels tests concurrent requests to different models.
func TestConcurrentRequestsDifferentModels(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{
			"model-a",
			"model-b",
			"model-c",
		}, "anthropic").
		WithRoute("model-a", "mock:model-a").
		WithRoute("model-b", "mock:model-b").
		WithRoute("model-c", "mock:model-c").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-models")

	models := []string{"model-a", "model-b", "model-c"}
	totalRequests := 30
	results := make(chan int, totalRequests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			model := models[id%len(models)]

			reqBody := map[string]any{
				"model":      model,
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d to %s", id, model)}},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent requests to different models: %d/%d succeeded in %v", successCount, totalRequests, duration)

	if successCount != totalRequests {
		t.Errorf("Expected all %d requests to succeed, got %d", totalRequests, successCount)
	}
}

// TestConcurrentLargeRequests tests concurrent requests with large payloads.
func TestConcurrentLargeRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Large response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 1000, "output_tokens": 500},
		})
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
		APIKey:          "test-key",
		Timeout:         "60s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-large")

	concurrency := 10
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	// Create a large message (about 10KB)
	largeContent := string(make([]byte, 10000))
	for i := range largeContent {
		largeContent = largeContent[:i] + "a" + largeContent[i+1:]
	}

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 500,
				"messages": []map[string]any{
					{"role": "user", "content": largeContent},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent large requests: %d/%d succeeded in %v", successCount, concurrency, duration)

	if successCount != concurrency {
		t.Errorf("Expected all %d large requests to succeed, got %d", concurrency, successCount)
	}
}

// TestConcurrentToolCallRequests tests concurrent requests with tool calls.
func TestConcurrentToolCallRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "tool_use", "id": "tool-1", "name": "calculator", "input": map[string]any{"expression": "2+2"}}},
			"model":       "test-model",
			"stop_reason": "tool_use",
			"usage":       map[string]int{"input_tokens": 25, "output_tokens": 15},
		})
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-tools")

	concurrency := 20
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model": "test-model",
				"max_tokens": 100,
				"messages": []map[string]any{
					{"role": "user", "content": fmt.Sprintf("Calculate %d+%d", id, id)},
				},
				"tools": []map[string]any{
					{
						"name":        "calculator",
						"description": "Perform math calculations",
						"input_schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"expression": map[string]any{
									"type":        "string",
									"description": "Math expression",
								},
							},
							"required": []string{"expression"},
						},
					},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent tool call requests: %d/%d succeeded in %v", successCount, concurrency, duration)

	if successCount != concurrency {
		t.Errorf("Expected all %d tool call requests to succeed, got %d", concurrency, successCount)
	}
}

// TestConcurrentFailover tests concurrent requests with failover behavior.
func TestConcurrentFailover(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		// Fail 50% of requests
		if count%2 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"Primary down"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("primary-%d", count),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Primary response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("backup-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Backup response"}},
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
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	backupClient, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         backupServer.URL,
		APIKey:          "backup-key",
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{
		"primary": primaryClient,
		"backup":  backupClient,
	})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-failover")

	concurrency := 20
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", id)}},
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

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Concurrent failover: %d/%d requests succeeded in %v", successCount, concurrency, duration)

	if successCount != concurrency {
		t.Errorf("Expected all %d requests to succeed with failover, got %d", concurrency, successCount)
	}
}

// TestConcurrentContextCancellation tests concurrent requests with context cancellation.
func TestConcurrentContextCancellation(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Slow response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"mock": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-cancellation")

	concurrency := 10
	results := make(chan int, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Cancel half of the requests
			if id%2 == 0 {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()

				reqBody := map[string]any{
					"model":      "test-model",
					"max_tokens": 30,
					"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d (cancelled)", id)}},
				}
				body, _ := json.Marshal(reqBody)

				req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")

				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				// Cancelled requests may fail, that's OK
				if w.Code == http.StatusOK {
					results <- 1
				} else {
					results <- 0
				}
			} else {
				reqBody := map[string]any{
					"model":      "test-model",
					"max_tokens": 30,
					"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d (normal)", id)}},
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
			}
		}(i)
	}

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	// At least the non-cancelled requests should succeed
	minExpectedSuccess := concurrency / 2
	if successCount < minExpectedSuccess {
		t.Errorf("Expected at least %d successful requests, got %d", minExpectedSuccess, successCount)
	}

	t.Logf("Concurrent context cancellation: %d/%d requests succeeded in %v", successCount, concurrency, duration)
}