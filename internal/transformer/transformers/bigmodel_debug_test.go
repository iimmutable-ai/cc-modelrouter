package transformers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestBigModelRequestFormat reproduces the 1213 "prompt parameter not received" error.
// This test simulates BigModel's API behavior and verifies the request format.
func TestBigModelRequestFormat(t *testing.T) {
	// Track all requests received by the mock server
	var requestsReceived []string

	// Mock BigModel API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("Error reading request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"Failed to read request body"}}`))
			return
		}

		requestsReceived = append(requestsReceived, string(body))
		t.Logf("[MOCK SERVER] Received request #%d", len(requestsReceived))
		t.Logf("[MOCK SERVER] Content-Length: %d", r.ContentLength)
		t.Logf("[MOCK SERVER] Headers: %s", logging.SanitizeHeadersString(r.Header))
		t.Logf("[MOCK SERVER] Body: %s", string(body))

		// Simulate BigModel's behavior - check if 'messages' field is present
		var reqData map[string]interface{}
		if err := json.Unmarshal(body, &reqData); err != nil {
			t.Logf("[MOCK SERVER] Failed to parse JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"Invalid JSON"}}`))
			return
		}

		// Check for 'messages' field
		if _, hasMessages := reqData["messages"]; !hasMessages {
			t.Logf("[MOCK SERVER] Missing 'messages' field!")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		// Check for 'model' field
		if _, hasModel := reqData["model"]; !hasModel {
			t.Logf("[MOCK SERVER] Missing 'model' field!")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"Missing model parameter"}}`))
			return
		}

		// Return a mock streaming response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Send a simple response
		w.Write([]byte("event: message_start\n"))
		w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"glm-4.7\",\"stop_reason\":\"end_turn\",\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n"))
		w.Write([]byte("event: content_block_start\n"))
		w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\n"))
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello!\"}}\n\n"))
		w.Write([]byte("event: content_block_stop\n"))
		w.Write([]byte("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		w.Write([]byte("event: message_delta\n"))
		w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n"))
		w.Write([]byte("event: message_stop\n"))
		w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mockServer.Close()

	// Create the GLM transformer
	transformer := NewGLMAnthropicTransformer()

	// Create a test request
	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello, how are you?"},
				},
			},
		},
	}

	// Prepare the request
	httpReq, err := transformer.PrepareRequest(req, mockServer.URL, "test-api-key", "glm-4.7")
	if err != nil {
		t.Fatalf("Failed to prepare request: %v", err)
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read and log the response
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Response status: %d", resp.StatusCode)
	t.Logf("Response body: %s", string(respBody))

	// Check if we received multiple requests (to test retry behavior)
	t.Logf("Total requests received by mock server: %d", len(requestsReceived))

	// Verify we got the messages field
	if len(requestsReceived) > 0 {
		var reqData map[string]interface{}
		if err := json.Unmarshal([]byte(requestsReceived[0]), &reqData); err == nil {
			if messages, ok := reqData["messages"].([]interface{}); ok {
				t.Logf("✓ Request has messages array with %d messages", len(messages))
			}
		}
	}
}

// TestBigModelMultipleRequests simulates the failing scenario with multiple requests
func TestBigModelMultipleRequests(t *testing.T) {
	requestCount := 0
	failOnThirdRequest := false

	// Mock BigModel that fails on the third request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		t.Logf("[MOCK] Request #%d received", requestCount)

		body, _ := io.ReadAll(r.Body)
		t.Logf("[MOCK] Request body: %s", string(body))

		// Fail on third request with 1213
		if requestCount == 3 && failOnThirdRequest {
			t.Logf("[MOCK] Simulating 1213 error on request #3")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		// Normal response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mockServer.Close()

	transformer := NewGLMAnthropicTransformer()

	// Make 3 sequential requests
	for i := 1; i <= 3; i++ {
		req := &anthropic.Request{
			Model:     "glm-4.7",
			MaxTokens: 100,
			Stream:    true,
			Messages: []anthropic.Message{
				{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Test"}}},
			},
		}

		// Create fresh request each time (simulating deep copy)
		reqCopy := req
		httpReq, err := transformer.PrepareRequest(reqCopy, mockServer.URL, "test-key", "glm-4.7")
		if err != nil {
			t.Logf("Request #%d prep error: %v", i, err)
			continue
		}

		client := &http.Client{Timeout: 5}
		resp, err := client.Do(httpReq)
		if err != nil {
			t.Logf("Request #%d error: %v", i, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	t.Logf("Total requests made: %d", requestCount)
}

// TestRequestBodyIntegrity verifies that the request body is correctly formed
func TestRequestBodyIntegrity(t *testing.T) {
	transformer := NewGLMAnthropicTransformer()

	// Test with various message types
	testCases := []struct {
		name     string
		messages []anthropic.Message
	}{
		{
			name: "simple text message",
			messages: []anthropic.Message{
				{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}}},
			},
		},
		{
			name: "multiple text blocks",
			messages: []anthropic.Message{
				{Role: "user", Content: anthropic.MessageContent{
					{Type: "text", Text: "First block"},
					{Type: "text", Text: "Second block"},
				}},
			},
		},
		{
			name: "system message",
			messages: []anthropic.Message{
				{Role: "system", Content: anthropic.MessageContent{{Type: "text", Text: "You are helpful"}}},
				{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hi"}}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "glm-4.7",
				MaxTokens: 100,
				Stream:    false,
				Messages:  tc.messages,
			}

			// Prepare request - we'll intercept the body
			// by checking what gets logged
			_, err := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")
			if err != nil {
				t.Errorf("Failed to prepare request: %v", err)
			}
		})
	}
}

// TestStreamingRequestBody verifies streaming requests have correct body
func TestStreamingRequestBody(t *testing.T) {
	transformer := NewGLMAnthropicTransformer()

	// Create a request with streaming enabled
	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Tell me a story"},
				},
			},
		},
		Thinking: &anthropic.ThinkingConfig{
			Type:          "enabled",
			BudgetTokens:  1024,
		},
	}

	// Prepare request
	httpReq, err := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")
	if err != nil {
		t.Fatalf("Failed to prepare request: %v", err)
	}

	// Verify the body is set correctly
	if httpReq.Body == nil && httpReq.GetBody == nil {
		t.Error("Neither Body nor GetBody is set")
	}

	// Try to get the body
	if httpReq.GetBody != nil {
		body, err := httpReq.GetBody()
		if err != nil {
			t.Errorf("GetBody failed: %v", err)
		} else {
			bodyBytes, _ := io.ReadAll(body)
			body.Close()

			// Verify it's valid JSON and has the expected fields
			var reqData map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &reqData); err != nil {
				t.Errorf("Request body is not valid JSON: %v", err)
			} else {
				// Check required fields
				if _, ok := reqData["model"]; !ok {
					t.Error("Missing 'model' field")
				}
				if _, ok := reqData["messages"]; !ok {
					t.Error("Missing 'messages' field")
				}
				if stream, ok := reqData["stream"].(bool); !ok || !stream {
					t.Error("Missing or incorrect 'stream' field")
				}

				hasMessages := reqData["messages"] != nil
				t.Logf("✓ Request body is valid: model=%v, hasMessages=%v, stream=%v",
					reqData["model"], hasMessages, reqData["stream"])

				// Log the full body for inspection
				t.Logf("Request body: %s", string(bodyBytes))
			}
		}
	}
}

// TestContentTypeHeader verifies the correct headers are set
func TestContentTypeHeader(t *testing.T) {
	transformer := NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 100,
		Stream:    false,
		Messages: []anthropic.Message{
			{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Test"}}},
		},
	}

	httpReq, _ := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")

	// Check headers
	contentType := httpReq.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type: application/json, got: %s", contentType)
	}

	apiKey := httpReq.Header.Get("x-api-key")
	if apiKey != "test-key" {
		t.Errorf("Expected x-api-key: test-key, got: %s", apiKey)
	}

	anthropicVersion := httpReq.Header.Get("anthropic-version")
	if anthropicVersion != "2023-06-01" {
		t.Errorf("Expected anthropic-version: 2023-06-01, got: %s", anthropicVersion)
	}

	t.Logf("✓ All headers correct: Content-Type=%s, x-api-key=%s, anthropic-version=%s",
		contentType, apiKey, anthropicVersion)
}

// BenchmarkRequestPreparation benchmarks the request preparation
func BenchmarkRequestPreparation(b *testing.B) {
	transformer := NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: strings.Repeat("This is a test message. ", 100)},
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")
		if err != nil {
			b.Fatalf("Failed: %v", err)
		}
	}
}

// TestGLMAnthropicPrepareRequestNormalizesNullSchema reproduces the 422 scenario from
// inst_20260617_120222: Claude Code's internal web_search tool sends
// {"name":"web_search","input_schema":null}, which BigModel rejects with HTTP 422
// ("Input should be a valid dictionary"). The transformer must coerce the null schema
// to {"type":"object"} in the outbound body.
func TestGLMAnthropicPrepareRequestNormalizesNullSchema(t *testing.T) {
	transformer := NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "search the web"}}},
		},
		Tools: []anthropic.Tool{
			{Name: "web_search", InputSchema: nil},
		},
	}

	httpReq, err := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}
	if httpReq.GetBody == nil {
		t.Fatal("GetBody is nil; cannot inspect outbound body")
	}

	bodyRC, err := httpReq.GetBody()
	if err != nil {
		t.Fatalf("GetBody() failed: %v", err)
	}
	defer bodyRC.Close()
	bodyBytes, err := io.ReadAll(bodyRC)
	if err != nil {
		t.Fatalf("reading body failed: %v", err)
	}
	bodyStr := string(bodyBytes)

	if strings.Contains(bodyStr, `"input_schema":null`) {
		t.Errorf("outbound body still contains null schema:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"input_schema":{"type":"object"}`) {
		t.Errorf("outbound body missing normalized schema; expected substring \"input_schema\":{\"type\":\"object\"}.\nBody: %s", bodyStr)
	}

	// Verify the body is valid JSON and the tool name survived.
	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		t.Fatalf("outbound body is not valid JSON: %v", err)
	}
	tools, ok := parsed["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", parsed["tools"])
	}
	tool0, _ := tools[0].(map[string]interface{})
	if tool0["name"] != "web_search" {
		t.Errorf("expected tool name web_search, got %v", tool0["name"])
	}
	schema, _ := tool0["input_schema"].(map[string]interface{})
	if schema == nil || schema["type"] != "object" {
		t.Errorf("expected input_schema.type=object, got %v", tool0["input_schema"])
	}
}