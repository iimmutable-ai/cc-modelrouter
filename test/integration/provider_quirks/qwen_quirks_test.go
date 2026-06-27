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

// TestQwenFormatDifferences tests handling Qwen's unique response format.
func TestQwenFormatDifferences(t *testing.T) {
	// Qwen uses OpenAI-like format with some differences
	qwenResponse := `{
		"id": "chatcmpl-test123",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-plus",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "你好，我是通义千问。"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 15,
			"total_tokens": 25
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-plus"}, "qwen").
		WithRoute("qwen-plus", "qwen:qwen-plus").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-quirks")

	// Qwen uses OpenAI-like format
	reqBody := map[string]any{
		"model": "qwen-plus",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen format differences test: status=%d, body: %s", w.Code, w.Body.String())
}

// TestQwenToolCallFormat tests Qwen's tool call format.
func TestQwenToolCallFormat(t *testing.T) {
	// Qwen uses tool_calls format similar to OpenAI
	qwenResponse := `{
		"id": "chatcmpl-test456",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-plus",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"tool_calls": [
						{
							"id": "call_123",
							"type": "function",
							"function": {
								"name": "calculator",
								"arguments": "{\"expression\": \"2+2\"}"
							}
						}
					]
				},
				"finish_reason": "tool_calls"
			}
		],
		"usage": {
			"prompt_tokens": 20,
			"completion_tokens": 15,
			"total_tokens": 35
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-plus"}, "qwen").
		WithRoute("qwen-plus", "qwen:qwen-plus").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-toolcall")

	reqBody := map[string]any{
		"model": "qwen-plus",
		"messages": []map[string]any{
			{"role": "user", "content": "What's 2+2?"},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "calculator",
					"description": "Perform calculations",
					"parameters": map[string]any{
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
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen tool call format test: status=%d, body: %s", w.Code, w.Body.String())
}

// TestQwenChineseSupport tests Qwen's Chinese language support.
func TestQwenChineseSupport(t *testing.T) {
	qwenResponse := `{
		"id": "chatcmpl-chinese",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-plus",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "你好！我是通义千问，阿里云开发的大语言模型。"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 25,
			"total_tokens": 40
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-plus"}, "qwen").
		WithRoute("qwen-plus", "qwen:qwen-plus").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-chinese")

	reqBody := map[string]any{
		"model": "qwen-plus",
		"messages": []map[string]any{
			{"role": "user", "content": "请用中文介绍一下你自己"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Log("Qwen Chinese support test passed")
	}
}

// TestQwenStreamingFormat tests Qwen's streaming format.
func TestQwenStreamingFormat(t *testing.T) {
	// Qwen's streaming format
	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":"，我是"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":"通义千问"}}],"finish_reason":null}` + "\n\n"))
		w.Write([]byte(`data: {"id":"chatcmpl-test","choices":[{"index":0,"finish_reason":"stop"}]}` + "\n\n"))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-turbo"}, "qwen").
		WithRoute("qwen-turbo", "qwen:qwen-turbo").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-streaming")

	reqBody := map[string]any{
		"model":    "qwen-turbo",
		"stream":   true,
		"messages": []map[string]any{
			{"role": "user", "content": "你好"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen streaming test: status=%d", w.Code)
}

// TestQwenMultipleModels tests different Qwen models.
func TestQwenMultipleModels(t *testing.T) {
	models := []string{"qwen-turbo", "qwen-plus", "qwen-max"}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"id":          "chatcmpl-" + model,
					"object":      "chat.completion",
					"created":     1234567890,
					"model":       model,
					"choices": []map[string]any{
						{
							"index": 0,
							"message": map[string]any{
								"role":    "assistant",
								"content": "Response from " + model,
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
			defer qwenServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("qwen", qwenServer.URL, "test-key", []string{model}, "qwen").
				WithRoute(model, "qwen:"+model).
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         qwenServer.URL,
				APIKey:          "test-key",
				Timeout:         "30s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(transformers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-qwen-" + strings.ReplaceAll(model, "-", "_"))

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
				t.Logf("Qwen %s test passed", model)
			}
		})
	}
}

// TestQwenThinkingContent tests Qwen's thinking/reasoning content.
func TestQwenThinkingContent(t *testing.T) {
	// Qwen may include thinking/reasoning in responses
	qwenResponse := `{
		"id": "chatcmpl-thinking",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-plus",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "<thinking>Let me think about this...</thinking>Here's my answer."
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 20,
			"total_tokens": 35
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-plus"}, "qwen").
		WithRoute("qwen-plus", "qwen:qwen-plus").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-thinking")

	reqBody := map[string]any{
		"model": "qwen-plus",
		"messages": []map[string]any{
			{"role": "user", "content": "Explain your reasoning"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen thinking content test: status=%d", w.Code)
}

// TestQwenImageContent tests Qwen's image content support.
func TestQwenImageContent(t *testing.T) {
	// Qwen supports multimodal content
	qwenResponse := `{
		"id": "chatcmpl-image",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-vl-max",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "I can see the image. It shows a cat."
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 15,
			"total_tokens": 115
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-vl-max"}, "qwen").
		WithRoute("qwen-vl-max", "qwen:qwen-vl-max").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-image")

	// Multimodal request with image
	reqBody := map[string]any{
		"model": "qwen-vl-max",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "What's in this image?"},
					{"type": "image_url", "image_url": map[string]string{"url": "https://example.com/image.jpg"}},
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen image content test: status=%d", w.Code)
}

// TestQwenExtendedTokenSupport tests Qwen's extended token support.
func TestQwenExtendedTokenSupport(t *testing.T) {
	// Qwen supports large context windows
	qwenResponse := `{
		"id": "chatcmpl-extended",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "qwen-long",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Response from extended context model"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 10000,
			"completion_tokens": 100,
			"total_tokens": 10100
		}
	}`

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(qwenResponse))
	}))
	defer qwenServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("qwen", qwenServer.URL, "test-key", []string{"qwen-long"}, "qwen").
		WithRoute("qwen-long", "qwen:qwen-long").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         qwenServer.URL,
		APIKey:          "test-key",
		Timeout:         "60s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"qwen": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-qwen-extended")

	reqBody := map[string]any{
		"model": "qwen-long",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Qwen extended token test: status=%d", w.Code)
}