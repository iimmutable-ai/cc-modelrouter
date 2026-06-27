//go:build integration_real
// +build integration_real

package real_api

import (
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// ========================================
// ALIYUN TESTS (Aliyun API - Anthropic-compatible)
// ========================================

// TestAliyunGLM47 tests simple completion with Aliyun (MiniMax-M2.5).
func TestAliyunGLM47(t *testing.T) {
	skipIfNoKey(t, "aliyun")

	apiKey := getAPIKey("aliyun")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"MiniMax-M2.5",
			"glm-4.7",
		}, "anthropic").
		WithRoute("default", "aliyun:MiniMax-M2.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "test-aliyun-glm47")

	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' and tell me your name"},
		},
	}

	runRequestTest(t, handler, reqBody, "Aliyun GLM-4.7 simple completion")
}

// TestAliyunGLM47Streaming tests streaming response with Aliyun (MiniMax-M2.5).
func TestAliyunGLM47Streaming(t *testing.T) {
	skipIfNoKey(t, "aliyun")

	apiKey := getAPIKey("aliyun")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"MiniMax-M2.5",
			"glm-4.7",
		}, "anthropic").
		WithRoute("default", "aliyun:MiniMax-M2.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "test-aliyun-glm47-streaming")

	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello' in exactly one word"},
		},
	}

	runStreamingTest(t, handler, reqBody, "Aliyun GLM-4.7 streaming")
}

// TestAliyunMiniMaxM25 tests MiniMax-M2.5 model specifically.
func TestAliyunMiniMaxM25(t *testing.T) {
	skipIfNoKey(t, "aliyun")

	apiKey := getAPIKey("aliyun")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"MiniMax-M2.5",
			"glm-4.7",
		}, "anthropic").
		WithRoute("default", "aliyun:MiniMax-M2.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "test-aliyun-minimax-m25")

	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": "Identify yourself by model name"},
		},
	}

	runRequestTest(t, handler, reqBody, "Aliyun MiniMax-M2.5 model identification")
}

// TestAliyunMiniMaxM25Streaming tests streaming with MiniMax-M2.5.
func TestAliyunMiniMaxM25Streaming(t *testing.T) {
	skipIfNoKey(t, "aliyun")

	apiKey := getAPIKey("aliyun")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"MiniMax-M2.5",
			"glm-4.7",
		}, "anthropic").
		WithRoute("default", "aliyun:MiniMax-M2.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "test-aliyun-minimax-m25-streaming")

	reqBody := map[string]any{
		"model":      "MiniMax-M2.5",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Count from 1 to 5"},
		},
	}

	runStreamingTest(t, handler, reqBody, "Aliyun MiniMax-M2.5 streaming")
}

// TestAliyunToolCall tests tool calling with Aliyun.
func TestAliyunToolCall(t *testing.T) {
	skipIfNoKey(t, "aliyun")

	apiKey := getAPIKey("aliyun")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"MiniMax-M2.5",
			"glm-4.7",
		}, "anthropic").
		WithRoute("default", "aliyun:MiniMax-M2.5").
		Build()

	handler := createTestHandler(t, cfg, apiKey, "https://coding.dashscope.aliyuncs.com/apps/anthropic", "test-aliyun-tool")

	reqBody := map[string]any{
		"model": "MiniMax-M2.5",
		"max_tokens": 200,
		"messages": []map[string]any{
			{"role": "user", "content": "What's 2+2? Use a calculator tool."},
		},
		"tools": []map[string]any{
			{
				"name":        "calculator",
				"description": "Perform mathematical calculations",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expression": map[string]any{
							"type":        "string",
							"description": "The mathematical expression to evaluate",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
	}

	runToolCallTest(t, handler, reqBody, "Aliyun tool call")
}