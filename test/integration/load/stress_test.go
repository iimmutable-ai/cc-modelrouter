//go:build load
// +build load

package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestHighVolumeRequests tests handling high volume of requests sequentially.
func TestHighVolumeRequests(t *testing.T) {
	requestCount := int64(0)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", atomic.LoadInt64(&requestCount)),
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
	handler.SetInstanceID("test-high-volume")

	totalRequests := 100
	var successCount int64
	var failureCount int64

	start := time.Now()

	for i := 0; i < totalRequests; i++ {
		reqBody := map[string]any{
			"model":      "test-model",
			"max_tokens": 30,
			"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", i)}},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			atomic.AddInt64(&successCount, 1)
		} else {
			atomic.AddInt64(&failureCount, 1)
		}

		// Small delay to avoid overwhelming
		time.Sleep(5 * time.Millisecond)
	}

	duration := time.Since(start)

	t.Logf("High volume requests: %d succeeded, %d failed out of %d in %v (%.2f req/s)",
		atomic.LoadInt64(&successCount),
		atomic.LoadInt64(&failureCount),
		totalRequests,
		duration,
		float64(totalRequests)/duration.Seconds())

	if atomic.LoadInt64(&successCount) != int64(totalRequests) {
		t.Errorf("Expected all %d requests to succeed, got %d", totalRequests, atomic.LoadInt64(&successCount))
	}
}

// TestResourceLimits tests behavior under resource constraints.
func TestResourceLimits(t *testing.T) {
	var activeConnections int64
	var peakConnections int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := atomic.AddInt64(&activeConnections, 1)

		// Track peak
		for {
			current := atomic.LoadInt64(&peakConnections)
			if conn <= current || atomic.CompareAndSwapInt64(&peakConnections, current, conn) {
				break
			}
		}

		// Simulate work
		time.Sleep(100 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", conn),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})

		atomic.AddInt64(&activeConnections, -1)
	}))
	defer mockServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", mockServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	// Use limited connections
	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    10, // Limited to 10 connections
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
	handler.SetInstanceID("test-resource-limits")

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

	t.Logf("Resource limits test: %d/%d succeeded in %v, peak connections: %d (limit: 10)",
		successCount, concurrency, duration, atomic.LoadInt64(&peakConnections))

	if successCount != concurrency {
		t.Errorf("Expected all %d requests to succeed even with limited connections, got %d", concurrency, successCount)
	}

	// Peak should be close to our connection limit
	if atomic.LoadInt64(&peakConnections) > 20 {
		t.Logf("Warning: Peak connections %d exceeded expected limit of ~20", atomic.LoadInt64(&peakConnections))
	}
}

// TestMemoryStress tests handling of memory-intensive operations.
func TestMemoryStress(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return large responses
		largeContent := make([]byte, 100000) // 100KB per response
		for i := range largeContent {
			largeContent[i] = byte('a' + (i % 26))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": string(largeContent)}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 1000, "output_tokens": 50000},
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
	handler.SetInstanceID("test-memory-stress")

	requests := 20
	results := make(chan int, requests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Also send large request
			largeContent := string(make([]byte, 50000))

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 50000,
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

	t.Logf("Memory stress test: %d/%d requests with large payloads succeeded in %v",
		successCount, requests, duration)

	if successCount != requests {
		t.Errorf("Expected all %d memory-intensive requests to succeed, got %d", requests, successCount)
	}
}

// TestTimeoutUnderLoad tests timeout behavior under high load.
func TestTimeoutUnderLoad(t *testing.T) {
	var requestCounter int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt64(&requestCounter, 1)

		// Slow down progressively
		delay := time.Duration(count%5) * 200 * time.Millisecond
		time.Sleep(delay)

		if delay >= 800*time.Millisecond {
			w.WriteHeader(http.StatusRequestTimeout)
			w.Write([]byte(`{"error":{"message":"Request timeout"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", count),
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
		WithRetryConfig(1, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         mockServer.URL,
		APIKey:          "test-key",
		Timeout:         "1s", // Short timeout
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
	handler.SetInstanceID("test-timeout-load")

	requests := 20
	results := make(chan int, requests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < requests; i++ {
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

	t.Logf("Timeout under load: %d/%d requests succeeded in %v (some failures expected)",
		successCount, requests, duration)

	// We expect some failures due to timeouts
	if successCount == requests {
		t.Log("Note: All requests succeeded despite timeouts - server may be too fast")
	} else if successCount == 0 {
		t.Error("All requests failed - timeout may be too aggressive")
	}
}

// TestLongRunningRequests tests handling of long-running requests.
func TestLongRunningRequests(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Long running request
		time.Sleep(3 * time.Second)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Long running response"}},
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
		Timeout:         "10s", // Longer timeout for long requests
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
	handler.SetInstanceID("test-long-running")

	requests := 5
	results := make(chan int, requests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Long request %d", id)}},
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

	t.Logf("Long running requests: %d/%d succeeded in %v (%.2f avg sec per request)",
		successCount, requests, duration, duration.Seconds()/float64(requests))

	if successCount != requests {
		t.Errorf("Expected all %d long-running requests to succeed, got %d", requests, successCount)
	}
}

// TestSustainedLoad tests sustained load over time.
func TestSustainedLoad(t *testing.T) {
	requestCount := int64(0)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", atomic.LoadInt64(&requestCount)),
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
	handler.SetInstanceID("test-sustained-load")

	duration := 5 * time.Second
	requestInterval := 100 * time.Millisecond
	expectedRequests := int(duration / requestInterval)

	var successCount int64
	var failureCount int64

	start := time.Now()
	endTime := start.Add(duration)

	requestNum := 0
	for time.Now().Before(endTime) {
		requestNum++

		reqBody := map[string]any{
			"model":      "test-model",
			"max_tokens": 30,
			"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Request %d", requestNum)}},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			atomic.AddInt64(&successCount, 1)
		} else {
			atomic.AddInt64(&failureCount, 1)
		}

		time.Sleep(requestInterval)
	}

	actualDuration := time.Since(start)

	t.Logf("Sustained load test: %d requests sent, %d succeeded, %d failed over %v (%.2f req/s)",
		requestNum,
		atomic.LoadInt64(&successCount),
		atomic.LoadInt64(&failureCount),
		actualDuration,
		float64(requestNum)/actualDuration.Seconds())

	if atomic.LoadInt64(&successCount) < int64(expectedRequests*90/100) {
		t.Errorf("Expected at least 90%% success rate, got %.2f%%",
			float64(atomic.LoadInt64(&successCount))/float64(requestNum)*100)
	}
}

// TestBurstLoad tests handling of sudden burst of requests.
func TestBurstLoad(t *testing.T) {
	var requestCount int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("msg-%d", atomic.LoadInt64(&requestCount)),
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
	handler.SetInstanceID("test-burst-load")

	burstSize := 100
	results := make(chan int, burstSize)
	var wg sync.WaitGroup

	// Launch all requests at once (burst)
	for i := 0; i < burstSize; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 30,
				"messages":   []map[string]any{{"role": "user", "content": fmt.Sprintf("Burst request %d", id)}},
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

	start := time.Now()

	wg.Wait()
	close(results)

	duration := time.Since(start)

	successCount := 0
	for result := range results {
		successCount += result
	}

	t.Logf("Burst load test: %d/%d requests succeeded in %v (%.2f req/s burst rate)",
		successCount, burstSize, duration, float64(burstSize)/duration.Seconds())

	if successCount < burstSize*95/100 {
		t.Errorf("Expected at least 95%% success rate during burst, got %d/%d", successCount, burstSize)
	}
}

// TestLoadWithFailover tests load handling with failover providers.
func TestLoadWithFailover(t *testing.T) {
	var primaryCount int64
	var backupCount int64

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&primaryCount, 1)
		// Fail 30% of requests
		if atomic.LoadInt64(&primaryCount)%10 < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"Primary overloaded"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("primary-%d", atomic.LoadInt64(&primaryCount)),
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
		atomic.AddInt64(&backupCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          fmt.Sprintf("backup-%d", atomic.LoadInt64(&backupCount)),
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
	handler.SetInstanceID("test-load-failover")

	requests := 50
	results := make(chan int, requests)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < requests; i++ {
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

	t.Logf("Load with failover: %d/%d succeeded in %v (primary: %d, backup: %d)",
		successCount, requests, duration,
		atomic.LoadInt64(&primaryCount), atomic.LoadInt64(&backupCount))

	if successCount != requests {
		t.Errorf("Expected all %d requests to succeed with failover, got %d", requests, successCount)
	}

	// Check that backup was actually used
	if atomic.LoadInt64(&backupCount) == 0 {
		t.Error("Backup provider was not used despite primary failures")
	}
}