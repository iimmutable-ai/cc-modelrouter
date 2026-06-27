//go:build network
// +build network

package network

import (
	"bytes"
	"encoding/json"
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

// TestPartialResponse tests handling of truncated/incomplete responses.
func TestPartialResponse(t *testing.T) {
	// Create a server that sends incomplete JSON
	partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test-id","type":"message","role":"assistant","content":[{"type":"text","text":"Hello`))
	}))
	defer partialServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("partial-provider", partialServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "partial-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         partialServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"partial-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-partial-response")

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

	// Should handle partial response gracefully with error
	if w.Code == http.StatusOK {
		t.Error("Expected error for partial response, got success")
	}

	t.Logf("Partial response test completed with status: %d, body: %s", w.Code, w.Body.String())
}

// TestMidStreamFailure tests handling of failures during streaming responses.
func TestMidStreamFailure(t *testing.T) {
	streamFailure := false

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}` + "\n\n"))

		if !streamFailure {
			// Fail mid-stream
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"Stream failed"}}`))
			streamFailure = true
			return
		}

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer failureServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("stream-fail", failureServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "stream-fail:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         failureServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"stream-fail": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-midstream-failure")

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

	t.Logf("Mid-stream failure test completed with status: %d", w.Code)
}

// TestEmptyResponse tests handling of empty response bodies.
func TestEmptyResponse(t *testing.T) {
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Send empty body with 200 status
		w.WriteHeader(http.StatusOK)
	}))
	defer emptyServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("empty-provider", emptyServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "empty-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         emptyServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"empty-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-empty-response")

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

	// Should handle empty response gracefully
	t.Logf("Empty response test completed with status: %d", w.Code)
}

// TestMalformedJSON tests handling of malformed JSON responses.
func TestMalformedJSON(t *testing.T) {
	malformedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Send malformed JSON
		w.Write([]byte(`{"id":"test-id","type":"message", INVALID JSON`))
	}))
	defer malformedServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("malformed-provider", malformedServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "malformed-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         malformedServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"malformed-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-malformed-json")

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

	// Should handle malformed JSON with error
	if w.Code == http.StatusOK {
		t.Error("Expected error for malformed JSON, got success")
	}

	t.Logf("Malformed JSON test completed with status: %d", w.Code)
}

// TestUnexpectedContentType tests handling of non-JSON content types.
func TestUnexpectedContentType(t *testing.T) {
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("This is not JSON"))
	}))
	defer textServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("text-provider", textServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "text-provider:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         textServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"text-provider": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-unexpected-content-type")

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

	// Should handle unexpected content type
	t.Logf("Unexpected content type test completed with status: %d", w.Code)
}

// TestLargePartialResponse tests handling of large responses that get truncated.
func TestLargePartialResponse(t *testing.T) {
	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Start valid JSON
		w.Write([]byte(`{"id":"test-id","type":"message","role":"assistant","content":[{"type":"text","text":"`))

		// Write lots of content
		for i := 0; i < 1000; i++ {
			w.Write([]byte("This is a long response that will be truncated. "))
		}

		// Don't close the JSON properly
	}))
	defer largeServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("large-partial", largeServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "large-partial:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         largeServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"large-partial": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-large-partial")

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

	t.Logf("Large partial response test completed with status: %d", w.Code)
}

// TestChunkedTransferEncodingFailure tests handling of chunked transfer failures.
func TestChunkedTransferEncodingFailure(t *testing.T) {
	chunkedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Transfer-Encoding", "chunked")

		// Send first chunk
		w.Write([]byte(`{"id":"test-id","type":"message","role":"assistant",`))

		// Connection fails before second chunk
	}))
	defer chunkedServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("chunked-fail", chunkedServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "chunked-fail:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         chunkedServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"chunked-fail": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-chunked-failure")

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

	t.Logf("Chunked transfer failure test completed with status: %d", w.Code)
}

// TestSSEEventLoss tests handling of missing SSE events in streaming.
func TestSSEEventLoss(t *testing.T) {
	sseLossServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send message_start
		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"test","type":"message","role":"assistant","content":[],"model":"test"}}` + "\n\n"))

		// Skip content_block_start

		// Send content_block_delta (should fail due to missing start)
		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer sseLossServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("sse-loss", sseLossServer.URL, "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "sse-loss:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         sseLossServer.URL,
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
	handler.SetProviderClients(map[string]proxy.HTTPClient{"sse-loss": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-sse-loss")

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

	t.Logf("SSE event loss test completed with status: %d", w.Code)
}