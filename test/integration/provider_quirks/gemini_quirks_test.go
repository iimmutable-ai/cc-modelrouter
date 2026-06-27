//go:build provider_quirks
// +build provider_quirks

package provider_quirks

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

// TestGeminiFormatDifferences tests handling Gemini's unique response format.
func TestGeminiFormatDifferences(t *testing.T) {
	// Gemini uses a different format: candidates array with content as parts
	geminiResponse := `{
		"candidates": [
			{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{"text": "Hello from Gemini"}
					]
				},
				"finishReason": "STOP"
			}
		],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(geminiResponse))
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-format")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Hello"},
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini format differences test: status=%d, body: %s", w.Code, w.Body.String())
}

// TestGeminiEmptyParts tests Gemini response with empty parts array.
func TestGeminiEmptyParts(t *testing.T) {
	geminiResponse := `{
		"candidates": [
			{
				"index": 0,
				"content": {
					"role": "model",
					"parts": []
				},
				"finishReason": "STOP"
			}
		],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 0,
			"totalTokenCount": 10
		}
	}`

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(geminiResponse))
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-empty-parts")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Hello"},
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini empty parts test: status=%d", w.Code)
}

// TestGeminiFunctionCalling tests Gemini's function calling format.
func TestGeminiFunctionCalling(t *testing.T) {
	// Gemini uses function_calling instead of tool_use
	geminiResponse := `{
		"candidates": [
			{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{
							"functionCall": {
								"name": "calculator",
								"args": {
									"expression": "2+2"
								}
							}
						}
					]
				},
				"finishReason": "STOP"
			}
		],
		"usageMetadata": {
			"promptTokenCount": 20,
			"candidatesTokenCount": 15,
			"totalTokenCount": 35
		}
	}`

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(geminiResponse))
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-func-calling")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "What's 2+2?"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
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
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini function calling test: status=%d, body: %s", w.Code, w.Body.String())
}

// TestGeminiStreamingFormat tests Gemini's streaming format.
func TestGeminiStreamingFormat(t *testing.T) {
	// Gemini's streaming format is different from Anthropic
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		w.Write([]byte(`data: {"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}` + "\n\n"))
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-streaming")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Hello"},
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini streaming test: status=%d, body: %s", w.Code, w.Body.String())
}

// TestGeminiSafetyFilters tests Gemini's safety filter responses.
func TestGeminiSafetyFilters(t *testing.T) {
	// Gemini may return safety filter responses
	geminiResponse := `{
		"candidates": [
			{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{"text": "I cannot assist with that request."}
					]
				},
				"finishReason": "SAFETY",
				"safetyRatings": [
					{
						"category": "HARM_CATEGORY_HARASSMENT",
						"probability": "HIGH"
					}
				]
			}
		],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 8,
			"totalTokenCount": 18
		}
	}`

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(geminiResponse))
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-safety")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Unsafe content"},
				},
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini safety filters test: status=%d", w.Code)
}

// TestGeminiGenerationConfig tests Gemini-specific generation config.
func TestGeminiGenerationConfig(t *testing.T) {
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for generationConfig in request
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		if genConfig, ok := reqBody["generationConfig"].(map[string]any); ok {
			t.Logf("Received generationConfig: %v", genConfig)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"index": 0,
					"content": map[string]any{
						"role": "model",
						"parts": []map[string]any{
							{"text": "Response"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount": 10,
				"candidatesTokenCount": 5,
				"totalTokenCount": 15,
			},
		})
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-genconfig")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Hello"},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.7,
			"topP":             0.9,
			"topK":             40,
			"maxOutputTokens": 100,
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini generation config test: status=%d", w.Code)
}

// TestGeminiSystemInstruction tests Gemini's system instruction format.
func TestGeminiSystemInstruction(t *testing.T) {
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		if sysInst, ok := reqBody["systemInstruction"]; ok {
			t.Logf("Received systemInstruction: %v", sysInst)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"index": 0,
					"content": map[string]any{
						"role": "model",
						"parts": []map[string]any{
							{"text": "Response with system instruction applied"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount": 20,
				"candidatesTokenCount": 10,
				"totalTokenCount": 30,
			},
		})
	}))
	defer geminiServer.Close()

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", geminiServer.URL, "test-key", []string{"gemini-2.0-flash-exp"}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()

	client, _ := provider.NewClient(&provider.ClientConfig{
		BaseURL:         geminiServer.URL,
		APIKey:          "test-key",
		Timeout:         "30s",
		MaxIdleConns:    100,
		IdleConnTimeout: "90s",
	})

	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGeminiTransformer())

	routerEngine := router.NewEngine(cfg)

	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
	handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
	handler.SetProviderClients(map[string]proxy.HTTPClient{"gemini": client})
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-gemini-sysinst")

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Hello"},
				},
			},
		},
		"systemInstruction": "You are a helpful assistant.",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Gemini system instruction test: status=%d", w.Code)
}