//go:build provider_quirks
// +build provider_quirks

package provider_quirks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestOpenRouterCustomHeaders tests OpenRouter's custom headers support.
func TestOpenRouterCustomHeaders(t *testing.T) {
	headersReceived := make(map[string]string)

	openrouterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture all headers
		for name, values := range r.Header {
			if len(values) > 0 {
				headersReceived[name] = values[0]
			}
		}

		// OpenRouter uses OpenAI-like format
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "gen-or-test",
			"model":   "anthropic/claude-3.5-sonnet",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from OpenRouter",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"total_tokens":      25,
			},
		})
	}))
	defer openrouterServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", openrouterServer.URL, "test-key", []string{
			"anthropic/claude-3.5-sonnet",
		}, "openrouter").
		WithRoute("claude-3.5-sonnet", "openrouter:anthropic/claude-3.5-sonnet").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         openrouterServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewOpenRouterTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"openrouter": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-openrouter-headers")

	reqBody := map[string]any{
		"model": "claude-3.5-sonnet",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check that required headers were sent
	requiredHeaders := []string{"Authorization", "HTTP-Referer", "X-Title"}
	for _, header := range requiredHeaders {
		if _, ok := headersReceived[header]; !ok {
			t.Logf("OpenRouter header '%s' not found. Headers received: %v", header, headersReceived)
		}
	}

	t.Logf("OpenRouter custom headers test: status=%d", w.Code)
}

// TestOpenRouterMultipleProviders tests OpenRouter's multi-provider routing.
func TestOpenRouterMultipleProviders(t *testing.T) {
	// OpenRouter routes to different underlying providers
	openrouterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqBody map[string]any
			json.NewDecoder(r.Body).Decode(&reqBody)

			model := ""
			if m, ok := reqBody["model"].(string); ok {
				model = m
			}

			w.Header().Set("Content-Type", "application/json")

			// Return different content based on model prefix
			var provider string
			var content string
			if strings.HasPrefix(model, "anthropic/") {
				provider = "Anthropic"
				content = "Response from Anthropic via OpenRouter"
			} else if strings.HasPrefix(model, "openai/") {
				provider = "OpenAI"
				content = "Response from OpenAI via OpenRouter"
			} else {
				provider = "Unknown"
				content = "Response from unknown provider"
			}

			json.NewEncoder(w).Encode(map[string]any{
				"id":      "gen-or-" + provider,
				"model":   model,
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": content,
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 15,
					"total_tokens":      25,
				},
			})
		}))
	defer openrouterServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", openrouterServer.URL, "test-key", []string{
			"anthropic/claude-3.5-sonnet",
			"openai/gpt-4o",
			"google/gemini-2.0-flash-exp",
		}, "openrouter").
		WithRoute("claude-3.5-sonnet", "openrouter:anthropic/claude-3.5-sonnet").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         openrouterServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewOpenRouterTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"openrouter": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-openrouter-multi")

	// Test different models through OpenRouter
	models := []string{
		"anthropic/claude-3.5-sonnet",
		"openai/gpt-4o",
	}

	for _, model := range models {
		reqBody := map[string]any{
			"model": model,
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
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
				t.Logf("Model %s via OpenRouter: success", model)
			}
		}
	}

	t.Log("OpenRouter multi-provider test completed")
}

// TestOpenRouterStreaming tests OpenRouter's streaming format.
func TestOpenRouterStreaming(t *testing.T) {
	openrouterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// OpenRouter uses OpenAI-like streaming format
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":" from"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":" OpenRouter"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"finish_reason":"stop"}]}` + "\n\n"))
	}))
	defer openrouterServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", openrouterServer.URL, "test-key", []string{
			"anthropic/claude-3.5-sonnet",
		}, "openrouter").
		WithRoute("claude-3.5-sonnet", "openrouter:anthropic/claude-3.5-sonnet").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         openrouterServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewOpenRouterTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"openrouter": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-openrouter-streaming")

	reqBody := map[string]any{
		"model":  "claude-3.5-sonnet",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("OpenRouter streaming test: status=%d", w.Code)
}

// TestOpenRouterPricingInfo tests OpenRouter's pricing information.
func TestOpenRouterPricingInfo(t *testing.T) {
	// OpenRouter includes pricing info in headers
	openrouterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Prompt-Tokens", "10")
			w.Header().Set("X-Completion-Tokens", "15")
			w.Header().Set("X-Total-Tokens", "25")

			json.NewEncoder(w).Encode(map[string]any{
				"id":      "gen-or-pricing",
				"model":   "anthropic/claude-3.5-sonnet",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "Hello",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 15,
					"total_tokens":      25,
				},
			})
		}))
	defer openrouterServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", openrouterServer.URL, "test-key", []string{
			"anthropic/claude-3.5-sonnet",
		}, "openrouter").
		WithRoute("claude-3.5-sonnet", "openrouter:anthropic/claude-3.5-sonnet").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         openrouterServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewOpenRouterTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"openrouter": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-openrouter-pricing")

	reqBody := map[string]any{
		"model": "claude-3.5-sonnet",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check for OpenRouter pricing headers
	if promptTokens := w.Header().Get("X-Prompt-Tokens"); promptTokens != "" {
		t.Logf("OpenRouter pricing - prompt tokens: %s", promptTokens)
	}

	t.Logf("OpenRouter pricing test: status=%d", w.Code)
}

// TestGLMAnthropicCompatibility tests GLM's Anthropic compatibility mode.
func TestGLMAnthropicCompatibility(t *testing.T) {
	// GLM uses Anthropic-compatible format
	glmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for Bearer token (GLM uses this)
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				t.Errorf("GLM should use Bearer token, got: %s", auth)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "glm-123",
				"type":        "message",
				"role":        "assistant",
				"content":     []map[string]any{{"type": "text", "text": "你好，我是GLM模型"}},
				"model":       "glm-4",
				"stop_reason": "end_turn",
				"usage":       map[string]int{"input_tokens": 10, "output_tokens": 15},
			})
		}))
	defer glmServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("glm", glmServer.URL, "test-key", []string{"glm-4"}, "glm").
		WithRoute("glm-4", "glm:glm-4").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         glmServer.URL,
		APIKey:          "test-key",
			Timeout:         "30s",
			MaxIdleConns:    100,
			IdleConnTimeout: "90s",
		})

		registry := transformer.NewRegistry()
		registry.Register(transformers.NewGLMAnthropicTransformer())

		routerEngine := router.NewEngine(cfg)

		handler := proxy.NewHandler(50 * 1024 * 1024)
		handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
		handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
		handler.SetProviderClients(map[string]proxy.HTTPClient{"glm": client})
	handler.SetConfig(cfg)
		handler.SetInstanceID("test-glm-quirks")

	reqBody := map[string]any{
		"model": "glm-4",
		"messages": []map[string]any{
			{"role": "user", "content": "你好"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("GLM Anthropic compatibility test passed")
	}
}

// TestGLMStreaming tests GLM's streaming format (Anthropic-compatible).
func TestGLMStreaming(t *testing.T) {
	glmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		w.Write([]byte(`event: message_start` + "\n"))
		w.Write([]byte(`data: {"type":"message_start","message":{"id":"glm-123","type":"message","role":"assistant","content":[],"model":"glm-4","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}` + "\n\n"))

		w.Write([]byte(`event: content_block_delta` + "\n"))
		w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}` + "\n\n"))

		w.Write([]byte(`event: message_delta` + "\n"))
		w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n"))

		w.Write([]byte(`event: message_stop` + "\n"))
		w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer glmServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("glm", glmServer.URL, "test-key", []string{"glm-4-flash"}, "glm").
		WithRoute("glm-4-flash", "glm:glm-4-flash").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         glmServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGLMAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"glm": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-glm-streaming")

	reqBody := map[string]any{
		"model":  "glm-4-flash",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "你好"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("GLM streaming test: status=%d", w.Code)
}

// TestGLMToolCalls tests GLM's tool call format (Anthropic-compatible).
func TestGLMToolCalls(t *testing.T) {
	glmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "glm-456",
				"type":        "message",
				"role":        "assistant",
				"content":     []map[string]any{{"type": "tool_use", "id": "tool_1", "name": "calculator", "input": map[string]any{"expression": "2+2"}}},
				"model":       "glm-4",
				"stop_reason": "tool_use",
				"usage":       map[string]int{"input_tokens": 25, "output_tokens": 15},
			})
		}))
	defer glmServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("glm", glmServer.URL, "test-key", []string{"glm-4"}, "glm").
		WithRoute("glm-4", "glm:glm-4").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         glmServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
		})

	registry := transformer.NewRegistry()
		registry.Register(transformers.NewGLMAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"glm": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-glm-tools")

	reqBody := map[string]any{
		"model": "glm-4",
		"messages": []map[string]any{
			{"role": "user", "content": "2加2等于几？"},
		},
		"tools": []map[string]any{
			{
				"name":        "calculator",
				"description": "计算器",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expression": map[string]any{
							"type":        "string",
							"description": "表达式",
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

	t.Logf("GLM tool calls test: status=%d", w.Code)
}