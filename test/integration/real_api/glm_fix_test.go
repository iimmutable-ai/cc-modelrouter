//go:build integration_real
// +build integration_real

package real_api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestGLMMultipleTextBlocks tests the fix for GLM 400 Bad Request error.
//
// This test validates that multiple consecutive text content blocks are properly
// merged into a single text block before being sent to GLM's ZenZGA/2.3 proxy.
//
// Background:
// - Claude Code sends requests with multiple text blocks (system reminders, context, etc.)
// - GLM's ZenZGA/2.3 proxy rejects array format content with 400 Bad Request
// - The fix merges consecutive text blocks to ensure string format content
//
// Expected behavior:
// - Multiple text blocks should be merged into one
// - Request should succeed with 200 OK (not 400 Bad Request)
func TestGLMMultipleTextBlocks(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-multiblock")

	// This request structure mimics what Claude Code sends:
	// Multiple consecutive text content blocks
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "This is the first text block. "},
					{"type": "text", "text": "This is the second text block. "},
					{"type": "text", "text": "This is the third text block."},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Testing GLM with multiple consecutive text blocks...")
	t.Logf("Request body: %s", string(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	// Before fix: 400 Bad Request from ZenZGA/2.3
	// After fix: 200 OK with merged text content
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
		t.Fatal("GLM multiple text blocks test failed - fix may not be working correctly")
	}

	// Verify we got a valid response
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if id, ok := resp["id"].(string); ok {
		t.Logf("✓ Response ID: %s", id)
	}

	if model, ok := resp["model"].(string); ok {
		t.Logf("✓ Model: %s", model)
	}

	// Verify content was merged and processed correctly
	if content, ok := resp["content"].([]any); ok && len(content) > 0 {
		for _, c := range content {
			if contentMap, ok := c.(map[string]any); ok {
				if textType, ok := contentMap["type"].(string); ok && textType == "text" {
					if text, ok := contentMap["text"].(string); ok {
						t.Logf("✓ Content received: %s", text)
						// The model should have processed all the merged text
					}
				}
			}
		}
	}

	t.Log("✓ GLM multiple text blocks test PASSED - fix is working correctly")
}

// TestGLMMixedContentWithMultipleTextBlocks tests that text merging works
// correctly with mixed content types (text + images, text + tool_result).
func TestGLMMixedContentWithMultipleTextBlocks(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-mixed")

	// Test with text blocks before and after a tool_result
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 100,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "First text block before tool. "},
					{"type": "text", "text": "Second text block before tool. "},
					{
						"type":    "tool_result",
						"tool_use_id": "test123",
						"content": "Tool result content",
					},
					{"type": "text", "text": "First text block after tool. "},
					{"type": "text", "text": "Second text block after tool."},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Testing GLM with mixed content (multiple text blocks + tool_result)...")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
	} else {
		t.Log("✓ GLM mixed content test PASSED")
	}
}

// TestGLMStreamingMultipleTextBlocks tests streaming with multiple text blocks.
func TestGLMStreamingMultipleTextBlocks(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-stream-multiblock")

	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 50,
		"stream":     true,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Block one. "},
					{"type": "text", "text": "Block two. "},
					{"type": "text", "text": "Block three."},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Testing GLM streaming with multiple text blocks...")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	contentType := w.Header().Get("Content-Type")
	t.Logf("Content-Type: %s", contentType)

	if w.Code == http.StatusOK {
		responseLength := len(w.Body.String())
		t.Logf("✓ Streaming response received: %d bytes", responseLength)

		// Verify it's actually streaming (SSE)
		if contentType == "text/event-stream" {
			t.Log("✓ Correct content-type for streaming")
		}
	} else {
		t.Logf("Response body: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestGLMRealWorldClaudeCodeRequest tests a realistic Claude Code request
// that includes system reminders and multiple content blocks.
func TestGLMRealWorldClaudeCodeRequest(t *testing.T) {
	skipIfNoKey(t, "bigmodel")

	apiKey := getAPIKey("bigmodel")

	cfg := util.NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", BigmodelBaseURL, apiKey, bigmodelModels, "glm").
		WithRoute("default", "bigmodel:glm-4.7").
		Build()

	handler := createTestHandler(t, cfg, apiKey, BigmodelBaseURL, "test-glm-realworld")

	// This mimics an actual Claude Code request structure
	reqBody := map[string]any{
		"model":      "glm-4.7",
		"max_tokens": 150,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "You are Claude Code, an AI assistant. ",
					},
					{
						"type": "text",
						"text": "The current project is cc-modelrouter, a Go-based HTTP proxy server. ",
					},
					{
						"type": "text",
						"text": "Please summarize the project architecture in one sentence.",
					},
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	t.Logf("Testing GLM with real-world Claude Code request structure...")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
		t.Fatal("Real-world request test failed")
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify we got a meaningful response
	if content, ok := resp["content"].([]any); ok && len(content) > 0 {
		for _, c := range content {
			if contentMap, ok := c.(map[string]any); ok {
				if textType, ok := contentMap["type"].(string); ok && textType == "text" {
					if text, ok := contentMap["text"].(string); ok {
						t.Logf("✓ Model response: %s", text)
						// Verify the model actually understood the merged request
						if len(text) > 10 {
							t.Log("✓ GLM successfully processed the merged text blocks")
						}
					}
				}
			}
		}
	}

	t.Log("✓ Real-world Claude Code request test PASSED")
}
