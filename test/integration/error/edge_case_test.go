//go:build edge_tests
// +build edge_tests

package error

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// TestEmptyMessages tests request with empty messages array.
func TestEmptyMessages(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "test-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hello"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 0, "output_tokens": 5},
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
	handler.SetInstanceID("test-empty-messages")

	reqBody := map[string]any{
		"model":    "test-model",
		"messages": []any{},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Empty messages test: status=%d", w.Code)
}

// TestZeroMaxTokens tests request with zero max_tokens.
func TestZeroMaxTokens(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "test-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": ""}},
			"model":       "test-model",
			"stop_reason": "max_tokens",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 0},
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
	handler.SetInstanceID("test-zero-maxtokens")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 0,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Zero max_tokens test: status=%d", w.Code)
}

// TestVeryLargeMaxTokens tests request with very large max_tokens.
func TestVeryLargeMaxTokens(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler.SetInstanceID("test-large-maxtokens")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100000000, // Very large
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Very large max_tokens test passed")
	}
}

// TestEmptyModel tests request with empty model name.
func TestEmptyModel(t *testing.T) {
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", "http://mock", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://mock",
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
	handler.SetInstanceID("test-empty-model")

	reqBody := map[string]any{
		"model":      "",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Empty model test: status=%d", w.Code)
}

// TestSpecialCharacters tests request with special characters.
func TestSpecialCharacters(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler.SetInstanceID("test-special-chars")

	specialChars := "你好世界 🌍 <>&\"' \\n\\t \\r\\0 \x00 \u0000"

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": specialChars},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Special characters test passed")
	}
}

// TestNullValues tests request with null values.
func TestNullValues(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler.SetInstanceID("test-null-values")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": nil,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
		"stream": nil,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Null values test: status=%d", w.Code)
}

// TestDeeplyNestedContent tests request with deeply nested content.
func TestDeeplyNestedContent(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler.SetInstanceID("test-nested-content")

	// Create deeply nested content
	nested := map[string]any{"text": "Hello"}
	for i := 0; i < 10; i++ {
		nested = map[string]any{"nested": nested}
	}

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
		"metadata": nested,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Deeply nested content test passed")
	}
}

// TestVeryLongModelName tests request with very long model name.
func TestVeryLongModelName(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer mockServer.Close()

	longModel := string(make([]byte, 1000))
	for i := range longModel {
		longModel = longModel[:i] + "a" + longModel[i+1:]
	}

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
	handler.SetInstanceID("test-long-model")

	reqBody := map[string]any{
		"model":      longModel,
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Very long model name test: status=%d", w.Code)
}

// TestManyTools tests request with many tools.
func TestManyTools(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "test-id",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Response"}},
			"model":       "test-model",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 5},
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
	handler.SetInstanceID("test-many-tools")

	// Create 50 tools
	tools := make([]map[string]any, 50)
	for i := 0; i < 50; i++ {
		tools[i] = map[string]any{
			"name":        fmt.Sprintf("tool_%d", i),
			"description": fmt.Sprintf("Tool number %d", i),
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"param": map[string]any{
						"type":        "string",
						"description": "Parameter",
					},
				},
				"required": []string{"param"},
			},
		}
	}

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
		"tools": tools,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Many tools test passed")
	}
}

// TestNegativeMaxTokens tests request with negative max_tokens.
func TestNegativeMaxTokens(t *testing.T) {
	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("mock", "http://mock", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "mock:test-model").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         "http://mock",
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
	handler.SetInstanceID("test-negative-maxtokens")

	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": -100,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Negative max_tokens test: status=%d", w.Code)
}

// TestExtraFields tests request with extra unknown fields.
func TestExtraFields(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler.SetInstanceID("test-extra-fields")

	reqBody := map[string]any{
		"model":              "test-model",
		"max_tokens":         100,
		"unknown_field_1":    "value1",
		"unknown_field_2":    123,
		"unknown_object":     map[string]any{"nested": true},
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Extra fields test passed")
	}
}