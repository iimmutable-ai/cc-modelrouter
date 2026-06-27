package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestError1213Reproduction reproduces the BigModel error 1213 "未正常接收到prompt参数".
//
// PRODUCTION PATTERN:
// - Large successful request (43748 tokens) completes
// - Immediate subsequent request fails with error 1213
// - Error is intermittent - not every request fails
// - Failover to Aliyun succeeds
//
// ROOT CAUSE HYPOTHESIS:
// The error may be related to:
// 1. Connection state management even with disableKeepAlives: true
// 2. Request body handling during sequential requests
// 3. Race condition in HTTP transport layer
// 4. BigModel API timing sensitivity
func TestError1213Reproduction(t *testing.T) {
	// Track all requests to simulate BigModel's behavior
	var requests []struct {
		body       string
		hasMessage bool
		hasModel   bool
		timestamp  time.Time
	}

	// Create a mock BigModel server that simulates the error condition
	mockBigModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read and validate request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("[MOCK BIGMODEL] Error reading body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"Failed to read request body"}}`))
			return
		}

		// Track this request
		var reqData map[string]interface{}
		json.Unmarshal(body, &reqData)
		requests = append(requests, struct {
			body       string
			hasMessage bool
			hasModel   bool
			timestamp  time.Time
		}{
			body:       string(body),
			hasMessage: reqData["messages"] != nil,
			hasModel:   reqData["model"] != nil,
			timestamp:  time.Now(),
		})

		t.Logf("[MOCK BIGMODEL] Request #%d: ContentLength=%d, HasMessages=%v, HasModel=%v",
			len(requests), r.ContentLength, reqData["messages"] != nil, reqData["model"] != nil)

		// Simulate BigModel's 1213 error pattern:
		// - After a large request (body > 100KB), the next request fails
		// - This mimics the production pattern where large requests seem to
		//   cause connection state issues
		if len(requests) > 1 && len(requests[len(requests)-2].body) > 100*1024 {
			t.Logf("[MOCK BIGMODEL] Simulating 1213 error after large request")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		// Successful response - SSE stream
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-test\"}}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mockBigModel.Close()

	// Create test handler with mock provider
	h := NewHandler(50 * 1024 * 1024) // 50MB max

	// Setup mock router using existing mockRouter type
	mockRouter := &mockRouter{
		getTargetsFunc: func(routeName string) []config.RouteTarget {
			return []config.RouteTarget{{Provider: "bigmodel", Model: "glm-4.7"}}
		},
	}
	h.SetRouter(mockRouter)

	// Setup transformer registry using existing mockTransformerRegistry type
	registry := transformer.NewRegistry()
	registry.Register(transformers.NewGLMAnthropicTransformer())
	h.SetTransformerRegistry(&mockTransformerRegistry{
		transformers: map[string]transformer.Transformer{
			"glm-anthropic": transformers.NewGLMAnthropicTransformer(),
		},
	})

	// Setup HTTP clients with disableKeepAlives=true (matching production)
	transport := &http.Transport{
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
	h.SetProviderClients(map[string]HTTPClient{"bigmodel": &httpClientAdapter{client: client}})
	h.SetStreamingClients(map[string]HTTPClient{"bigmodel": &httpClientAdapter{client: client}})

	// Set config
	h.SetConfig(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"bigmodel": {
				BaseURL:     mockBigModel.URL,
				APIKey:      "test-key",
				Transformer: "glm-anthropic",
				Models:      []string{"glm-4.7"},
			},
		},
	})

	// TEST SEQUENCE: Reproduce production pattern
	// 1. Large request (simulating 43748 tokens)
	largeContent := strings.Repeat("This is a test message for large request. ", 3000) // ~150KB
	largeReq := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: largeContent}}},
		},
	}

	// 2. Small subsequent request (should potentially fail after large)
	smallReq := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}}},
		},
	}

	// Execute large request first
	t.Log("=== Sending large request ===")
	largeReqBody, _ := json.Marshal(largeReq)
	req1 := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(largeReqBody))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	t.Logf("Large request response: %d", w1.Code)

	// Execute small request immediately after
	t.Log("=== Sending small request immediately after ===")
	smallReqBody, _ := json.Marshal(smallReq)
	req2 := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(smallReqBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	t.Logf("Small request response: %d", w2.Code)

	// Check if we reproduced the error
	t.Logf("Total requests sent: %d", len(requests))
	for i, req := range requests {
		t.Logf("Request #%d: HasMessages=%v, HasModel=%v, BodyLen=%d",
			i+1, req.hasMessage, req.hasModel, len(req.body))
	}

	// ANALYSIS: Even if we don't reproduce the exact error, log the behavior
	if w2.Code == 400 || w2.Code == 502 {
		body := w2.Body.String()
		if strings.Contains(body, "1213") {
			t.Log("SUCCESS: Reproduced error 1213")
		} else {
			t.Logf("Different error occurred: %s", body)
		}
	} else {
		t.Log("Note: Error not reproduced in this test environment")
		t.Log("This may require actual BigModel API to reproduce due to timing/network factors")
	}
}

// TestRequestBodyIntegrity tests that request body is correctly transmitted
// even under various connection state conditions.
func TestRequestBodyIntegrity(t *testing.T) {
	testCases := []struct {
		name        string
		requests    []*anthropic.Request
		description string
	}{
		{
			name: "single_small_request",
			requests: []*anthropic.Request{
				{
					Model:     "glm-4.7",
					MaxTokens: 100,
					Stream:    true,
					Messages:  []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}}}},
				},
			},
			description: "Single small request should work",
		},
		{
			name: "single_large_request",
			requests: []*anthropic.Request{
				{
					Model:     "glm-4.7",
					MaxTokens: 100,
					Stream:    true,
					Messages: []anthropic.Message{
						{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: strings.Repeat("Large content ", 5000)}}},
					},
				},
			},
			description: "Single large request should work",
		},
		{
			name: "multiple_sequential_requests",
			requests: []*anthropic.Request{
				{Model: "glm-4.7", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Request 1"}}}}},
				{Model: "glm-4.7", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Request 2"}}}}},
				{Model: "glm-4.7", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Request 3"}}}}},
			},
			description: "Multiple sequential requests should all work",
		},
		{
			name: "large_then_small_request",
			requests: []*anthropic.Request{
				{Model: "glm-4.7", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: strings.Repeat("Large ", 10000)}}}}},
				{Model: "glm-4.7", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Small"}}}}},
			},
			description: "Large request followed by small request (production pattern)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var requestBodies []string

			// Create mock server that captures all requests
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requestBodies = append(requestBodies, string(body))

				// Validate the request
				var reqData map[string]interface{}
				if err := json.Unmarshal(body, &reqData); err != nil {
					t.Errorf("[SERVER] Invalid JSON: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				// Check required fields
				if reqData["messages"] == nil {
					t.Errorf("[SERVER] Missing 'messages' field!")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
					return
				}
				if reqData["model"] == nil {
					t.Errorf("[SERVER] Missing 'model' field!")
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				// Success response
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("event: message_start\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"))
			}))
			defer mockServer.Close()

			// Create transformer
			tf := transformers.NewGLMAnthropicTransformer()

			// Send all requests
			for i, req := range tc.requests {
				httpReq, err := tf.PrepareRequest(req, mockServer.URL, "test-key", req.Model)
				if err != nil {
					t.Errorf("Request %d: Failed to prepare: %v", i, err)
					continue
				}

				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(httpReq)
				if err != nil {
					t.Errorf("Request %d: Failed to send: %v", i, err)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				t.Logf("Request %d: Status=%d, BodyLen=%d", i, resp.StatusCode, len(requestBodies[len(requestBodies)-1]))
			}

			// Verify all requests were received correctly
			if len(requestBodies) != len(tc.requests) {
				t.Errorf("Expected %d requests, received %d", len(tc.requests), len(requestBodies))
			}

			for i, body := range requestBodies {
				var reqData map[string]interface{}
				if err := json.Unmarshal([]byte(body), &reqData); err != nil {
					t.Errorf("Request %d: Body is not valid JSON", i)
					continue
				}
				if reqData["messages"] == nil {
					t.Errorf("Request %d: Missing messages field in transmitted body", i)
				}
				if reqData["model"] == nil {
					t.Errorf("Request %d: Missing model field in transmitted body", i)
				}
			}
		})
	}
}

// Helper function for string pointer
func strPtr(s string) *string {
	return &s
}

// TestDeepCopyIntegrity tests that deepCopyRequest creates truly independent copies.
func TestDeepCopyIntegrity(t *testing.T) {
	// Create original request with thinking blocks
	original := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello"},
					{Type: "thinking", Thinking: "Deep thought", Signature: strPtr("sig123")},
				},
			},
		},
	}

	// Create deep copy
	copy1, err := deepCopyRequest(original)
	if err != nil {
		t.Fatalf("Failed to deep copy: %v", err)
	}

	// Create another deep copy
	copy2, err := deepCopyRequest(original)
	if err != nil {
		t.Fatalf("Failed to deep copy: %v", err)
	}

	// Modify copy1
	copy1.Messages[0].Content[0].Text = "Modified"
	copy1.Messages[0].Content[1].Signature = strPtr("modified")

	// Verify original is unchanged
	if original.Messages[0].Content[0].Text != "Hello" {
		t.Error("Original was modified when copy1 was changed")
	}
	if *original.Messages[0].Content[1].Signature != "sig123" {
		t.Error("Original signature was modified when copy1 was changed")
	}

	// Verify copy2 is independent of copy1
	if copy2.Messages[0].Content[0].Text != "Hello" {
		t.Error("copy2 was modified when copy1 was changed")
	}
	if *copy2.Messages[0].Content[1].Signature != "sig123" {
		t.Error("copy2 signature was modified when copy1 was changed")
	}

	t.Log("Deep copy creates truly independent copies")
}

// TestGetBodyRestoration tests that GetBody properly restores the request body for retries.
func TestGetBodyRestoration(t *testing.T) {
	tf := transformers.NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: "Test message for GetBody"}}},
		},
	}

	httpReq, err := tf.PrepareRequest(req, "http://test.example.com", "test-key", "glm-4.7")
	if err != nil {
		t.Fatalf("Failed to prepare request: %v", err)
	}

	// Verify GetBody is set
	if httpReq.GetBody == nil {
		t.Fatal("GetBody is not set - retries will fail!")
	}

	// Get the original body
	body1, err := httpReq.GetBody()
	if err != nil {
		t.Fatalf("GetBody returned error: %v", err)
	}
	body1Bytes, _ := io.ReadAll(body1)
	body1.Close()

	// Get the body again (simulating retry)
	body2, err := httpReq.GetBody()
	if err != nil {
		t.Fatalf("GetBody returned error on second call: %v", err)
	}
	body2Bytes, _ := io.ReadAll(body2)
	body2.Close()

	// Both bodies should be identical
	if string(body1Bytes) != string(body2Bytes) {
		t.Errorf("Body content changed between calls!\nFirst: %s\nSecond: %s",
			string(body1Bytes), string(body2Bytes))
	}

	// Verify ContentLength is set
	if httpReq.ContentLength == 0 {
		t.Error("ContentLength is not set")
	}

	// Verify ContentLength matches actual body length
	if httpReq.ContentLength != int64(len(body1Bytes)) {
		t.Errorf("ContentLength (%d) doesn't match body length (%d)",
			httpReq.ContentLength, len(body1Bytes))
	}

	t.Logf("ContentLength: %d, Body length: %d", httpReq.ContentLength, len(body1Bytes))
	t.Log("GetBody properly restores request body for retries")
}

// TestTransportWithDisableKeepAlives tests behavior with DisableKeepAlives=true.
func TestTransportWithDisableKeepAlives(t *testing.T) {
	var requestCount int

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Check Connection header
		connection := r.Header.Get("Connection")
		t.Logf("Request #%d: Connection header: %q", requestCount, connection)

		body, _ := io.ReadAll(r.Body)

		var reqData map[string]interface{}
		json.Unmarshal(body, &reqData)

		if reqData["messages"] == nil {
			t.Errorf("Request #%d: Missing messages field", requestCount)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"))
	}))
	defer mockServer.Close()

	// Create client with DisableKeepAlives (matching BigModel production config)
	transport := &http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	tf := transformers.NewGLMAnthropicTransformer()

	// Send multiple requests
	for i := 0; i < 5; i++ {
		req := &anthropic.Request{
			Model:     "glm-4.7",
			MaxTokens: 100,
			Stream:    true,
			Messages: []anthropic.Message{
				{Role: "user", Content: anthropic.MessageContent{{Type: "text", Text: fmt.Sprintf("Request %d", i+1)}}},
			},
		}

		httpReq, err := tf.PrepareRequest(req, mockServer.URL, "test-key", "glm-4.7")
		if err != nil {
			t.Errorf("Request %d: Failed to prepare: %v", i+1, err)
			continue
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			t.Errorf("Request %d: Failed to send: %v", i+1, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// All requests should succeed
	if requestCount != 5 {
		t.Errorf("Expected 5 requests, got %d", requestCount)
	}

	t.Log("All requests succeeded with DisableKeepAlives=true")
}