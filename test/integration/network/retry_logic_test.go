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

// TestExponentialBackoff tests that retry delays increase exponentially.
func TestExponentialBackoff(t *testing.T) {
	attempts := 0
	attemptTimes := make([]time.Time, 0)

	// Create a server that records attempt times
	timingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		attemptTimes = append(attemptTimes, time.Now())

		// Fail first attempts, succeed on 3rd
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Rate limited"}}`))
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
	defer timingServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("retry-provider", timingServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "retry-provider:test-model").
		WithRetryConfig(5, "100ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         timingServer.URL,
		APIKey:          "test-key",
		Timeout:         "5s",
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"retry-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-exponential-backoff")

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
		t.Errorf("Expected success after retries, got status %d", w.Code)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	// Check that retry delays increased
	if len(attemptTimes) >= 3 {
		delay1 := attemptTimes[1].Sub(attemptTimes[0])
		delay2 := attemptTimes[2].Sub(attemptTimes[1])

		t.Logf("Attempt delays: %v, %v", delay1, delay2)
		// Second delay should be at least as long as first (exponential backoff)
		if delay2 < delay1 {
			t.Error("Expected exponential backoff, second delay was shorter")
		}
	}

	t.Logf("Exponential backoff test passed, total time: %v, attempts: %d", duration, attempts)
}

// TestMaxRetries tests that the retry limit is respected.
func TestMaxRetries(t *testing.T) {
	attempts := 0

	// Create a server that always fails
	alwaysFailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"Permanent failure"}}`))
	}))
	defer alwaysFailServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("fail-provider", alwaysFailServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "fail-provider:test-model").
		WithRetryConfig(3, "50ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         alwaysFailServer.URL,
		APIKey:          "test-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      3,
		RetryDelay:      50 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"fail-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-max-retries")

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
		t.Error("Expected failure after max retries, got success")
	}

	// Should have attempted max_retries + 1 times (initial + retries)
	expectedAttempts := 4
	if attempts > expectedAttempts {
		t.Errorf("Expected at most %d attempts, got %d", expectedAttempts, attempts)
	}

	t.Logf("Max retries test passed, attempts: %d", attempts)
}

// TestRetryOnSpecificErrors tests that retries only happen on retryable errors.
func TestRetryOnSpecificErrors(t *testing.T) {
	testCases := []struct {
		name           string
		statusCode     int
		shouldRetry    bool
		errorBody      string
	}{
		{
			name:        "429 rate limit",
			statusCode:  429,
			shouldRetry: true,
			errorBody:   `{"error":{"message":"Rate limit exceeded"}}`,
		},
		{
			name:        "500 server error",
			statusCode:  500,
			shouldRetry: true,
			errorBody:   `{"error":{"message":"Internal server error"}}`,
		},
		{
			name:        "502 bad gateway",
			statusCode:  502,
			shouldRetry: true,
			errorBody:   `{"error":{"message":"Bad gateway"}}`,
		},
		{
			name:        "503 service unavailable",
			statusCode:  503,
			shouldRetry: true,
			errorBody:   `{"error":{"message":"Service unavailable"}}`,
		},
		{
			name:        "400 bad request",
			statusCode:  400,
			shouldRetry: false,
			errorBody:   `{"error":{"message":"Bad request"}}`,
		},
		{
			name:        "401 unauthorized",
			statusCode:  401,
			shouldRetry: false,
			errorBody:   `{"error":{"message":"Unauthorized"}}`,
		},
		{
			name:        "404 not found",
			statusCode:  404,
			shouldRetry: false,
			errorBody:   `{"error":{"message":"Not found"}}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(tc.errorBody))
			}))
			defer server.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("test-provider", server.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "test-provider:test-model").
				WithRetryConfig(3, "50ms").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         server.URL,
				APIKey:          "test-key",
				Timeout:         "2s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
				MaxRetries:      3,
				RetryDelay:      50 * time.Millisecond,
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"test-provider": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-specific-errors")

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

			if tc.shouldRetry {
				if attempts <= 1 {
					t.Errorf("Expected retry for %s, got %d attempts", tc.name, attempts)
				}
			} else {
				if attempts > 1 {
					t.Errorf("Expected no retry for %s, got %d attempts", tc.name, attempts)
				}
			}

			t.Logf("Test %s: attempts=%d, expected retry=%v", tc.name, attempts, tc.shouldRetry)
		})
	}
}

// TestRetryTiming tests that retry delays are accurate.
func TestRetryTiming(t *testing.T) {
	testCases := []struct {
		name       string
		retryDelay string
		expected   time.Duration
	}{
		{"50ms delay", "50ms", 50 * time.Millisecond},
		{"100ms delay", "100ms", 100 * time.Millisecond},
		{"200ms delay", "200ms", 200 * time.Millisecond},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					w.Write([]byte(`{"error":{"message":"Rate limited"}}`))
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
			defer server.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("timing-provider", server.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "timing-provider:test-model").
				WithRetryConfig(3, tc.retryDelay).
				Build()

			parsedDelay, _ := time.ParseDuration(tc.retryDelay)

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         server.URL,
				APIKey:          "test-key",
				Timeout:         "10s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
				MaxRetries:      3,
				RetryDelay:      parsedDelay,
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"timing-provider": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-retry-timing")

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

			// Duration should be at least the retry delay
			minDuration := parsedDelay * 80 / 100 // Allow 20% tolerance
			if duration < minDuration {
				t.Errorf("Expected delay of at least %v, got %v", minDuration, duration)
			}

			t.Logf("Retry timing test %s: actual delay %v", tc.name, duration)
		})
	}
}

// TestRetryWithStreaming tests retry logic with streaming responses.
func TestRetryWithStreaming(t *testing.T) {
	attempts := 0

	streamingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"Rate limited"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer streamingServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("streaming-retry", streamingServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "streaming-retry:test-model").
		WithRetryConfig(3, "50ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         streamingServer.URL,
		APIKey:          "test-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      3,
		RetryDelay:      50 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"streaming-retry": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-streaming-retry")

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
		t.Errorf("Expected success after retries, got status %d", w.Code)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 attempts (initial + 1 retry), got %d", attempts)
	}

	t.Logf("Streaming retry test passed, attempts: %d", attempts)
}

// TestNoRetryOnSuccess tests that retries don't happen on success.
func TestNoRetryOnSuccess(t *testing.T) {
	attempts := 0

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
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
	defer successServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("success-provider", successServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "success-provider:test-model").
		WithRetryConfig(5, "50ms").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         successServer.URL,
		APIKey:          "test-key",
		Timeout:         "5s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		MaxRetries:      5,
		RetryDelay:      50 * time.Millisecond,
	})

	registry := transformer.NewRegistry()
	registry.Register(providers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"success-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-no-retry-success")

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
		t.Errorf("Expected success, got status %d", w.Code)
	}

	if attempts != 1 {
		t.Errorf("Expected 1 attempt (no retries), got %d", attempts)
	}

	t.Logf("No retry on success test passed, attempts: %d", attempts)
}