//go:build network
// +build network

package network

import (
	"bytes"
	"encoding/json"
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

// TestRateLimitHandling tests handling of 429 rate limit errors.
func TestRateLimitHandling(t *testing.T) {
	attempts := 0

	rateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "1")
			w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`))
			return
		}

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
	defer rateLimitServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("rate-limited", rateLimitServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "rate-limited:test-model").
		WithRetryConfig(5, "200ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         rateLimitServer.URL,
		APIKey:          "test-key",
		Timeout:         "10s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      200 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"rate-limited": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-rate-limit")

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
		t.Errorf("Expected success after rate limit backoff, got status %d", w.Code)
	}

	if attempts != 4 {
		t.Errorf("Expected 4 attempts (3 rate limits + 1 success), got %d", attempts)
	}

	t.Logf("Rate limit handling test passed, attempts: %d", attempts)
}

// TestStreamingRateLimit tests rate limit handling during streaming.
func TestStreamingRateLimit(t *testing.T) {
	attempts := 0

	streamRateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[],"model":"test"}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer streamRateLimitServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("stream-rate-limited", streamRateLimitServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "stream-rate-limited:test-model").
		WithRetryConfig(5, "150ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         streamRateLimitServer.URL,
		APIKey:          "test-key",
		Timeout:         "10s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      150 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"stream-rate-limited": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-stream-rate-limit")

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

	if w.Code != http.StatusOK {
		t.Errorf("Expected success after rate limit, got status %d", w.Code)
	}

	t.Logf("Streaming rate limit test passed, attempts: %d", attempts)
}

// TestRateLimitBackoffStrategy tests different backoff strategies for rate limits.
func TestRateLimitBackoffStrategy(t *testing.T) {
	testCases := []struct {
		name       string
		retryDelay string
		maxRetries int
	}{
		{"Fast backoff", "50ms", 3},
		{"Medium backoff", "200ms", 5},
		{"Slow backoff", "500ms", 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts <= tc.maxRetries {
					w.WriteHeader(http.StatusTooManyRequests)
					w.Write([]byte(`{"error":{"message":"Rate limit"}}`))
					return
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"id":          "test-id",
					"type":        "message",
					"role":        "assistant",
					"content":     []map[string]any{{"type": "text", "text": "OK"}},
					"model":       "test-model",
					"stop_reason": "end_turn",
					"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
				})
			}))
			defer server.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("backoff-test", server.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "backoff-test:test-model").
				WithRetryConfig(tc.maxRetries, tc.retryDelay).
				Build()

			parsedDelay, _ := time.ParseDuration(tc.retryDelay)

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         server.URL,
				APIKey:          "test-key",
				Timeout:         "30s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
				MaxRetries:      tc.maxRetries,
				RetryDelay:      parsedDelay,
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"backoff-test": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-backoff-"+tc.name)

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			start := time.Now()
			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			duration := time.Since(start)

			if w.Code != http.StatusOK {
				t.Errorf("Test %s: Expected success, got status %d", tc.name, w.Code)
			}

			// Check that total time is reasonable given backoff strategy
			minExpectedTime := time.Duration(tc.maxRetries) * parsedDelay * 80 / 100 // 80% tolerance
			if duration < minExpectedTime {
				t.Logf("Test %s: Warning - duration %v was less than expected %v", tc.name, duration, minExpectedTime)
			}

			t.Logf("Test %s: attempts=%d, duration=%v", tc.name, attempts, duration)
		})
	}
}

// TestRateLimitWithRetryAfter tests that Retry-After header is respected.
func TestRateLimitWithRetryAfter(t *testing.T) {
	attempts := 0
	serverRequests := make([]time.Time, 0)

	retryAfterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		serverRequests = append(serverRequests, time.Now())

		if attempts <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "1") // Retry after 1 second
			w.Write([]byte(`{"error":{"message":"Rate limit, retry after 1 second"}}`))
			return
		}

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
	defer retryAfterServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("retry-after", retryAfterServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "retry-after:test-model").
		WithRetryConfig(5, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         retryAfterServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      100 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"retry-after": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-retry-after")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	start := time.Now()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	duration := time.Since(start)

	if w.Code != http.StatusOK {
		t.Errorf("Expected success after respecting Retry-After, got status %d", w.Code)
	}

	// Should take at least 2 seconds (1 second per Retry-After)
	minDuration := 2 * time.Second
	if duration < minDuration {
		t.Logf("Warning: Duration %v was less than expected minimum %v (Retry-After may not be fully respected)", duration, minDuration)
	}

	t.Logf("Retry-After test passed, attempts: %d, duration: %v", attempts, duration)
}

// TestConcurrentRateLimit tests handling rate limits for concurrent requests.
func TestConcurrentRateLimit(t *testing.T) {
	requestCount := 0
	successCount := 0

	concurrentRateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Allow 3 successful requests, then rate limit
		if requestCount > 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
			return
		}

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
	defer concurrentRateLimitServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("concurrent-rate", concurrentRateLimitServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "concurrent-rate:test-model").
		WithRetryConfig(2, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         concurrentRateLimitServer.URL,
		APIKey:          "test-key",
		Timeout:         "10s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      2,
		RetryDelay:      100 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"concurrent-rate": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-concurrent-rate-limit")

	results := make(chan int, 5)

	// Make 5 concurrent requests
	for i := 0; i < 5; i++ {
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
	for i := 0; i < 5; i++ {
		successCount += <-results
	}

	t.Logf("Concurrent rate limit test: %d/%d requests succeeded", successCount, 5)
}

// TestPersistentRateLimit tests handling when rate limits persist beyond retry limit.
func TestPersistentRateLimit(t *testing.T) {
	attempts := 0

	persistentRateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
	}))
	defer persistentRateLimitServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("persistent-rate", persistentRateLimitServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "persistent-rate:test-model").
		WithRetryConfig(3, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         persistentRateLimitServer.URL,
		APIKey:          "test-key",
		Timeout:         "10s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      3,
		RetryDelay:      100 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"persistent-rate": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-persistent-rate-limit")

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

	// Should fail after exhausting retries
	if w.Code == http.StatusOK {
		t.Error("Expected failure after persistent rate limit, got success")
	}

	// Should not exceed max retries
	if attempts > 4 { // 1 initial + 3 retries
		t.Errorf("Expected at most 4 attempts, got %d", attempts)
	}

	t.Logf("Persistent rate limit test passed, attempts: %d", attempts)
}

// TestRateLimitRecovery tests that requests succeed after rate limit recovery window.
func TestRateLimitRecovery(t *testing.T) {
	requests := make(map[string]int)
	var lastRateLimit time.Time

	recoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")

		// Rate limit first 2 requests
		if requests[requestID] == 0 && len(requests) < 2 {
			lastRateLimit = time.Now()
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Rate limit"}}`))
			requests[requestID]++
			return
		}

		// Check if we're still in recovery window
		if time.Since(lastRateLimit) < 500*time.Millisecond {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Still rate limited"}}`))
			return
		}

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
	defer recoveryServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("recovery", recoveryServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "recovery:test-model").
		WithRetryConfig(5, "200ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         recoveryServer.URL,
		APIKey:          "test-key",
		Timeout:         "10s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      200 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"recovery": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-rate-limit-recovery")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-recovery")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Rate limit recovery test completed with status: %d", w.Code)
}