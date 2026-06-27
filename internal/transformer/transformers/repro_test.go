package transformers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestRepro_BigModel1213_BodyCorruption reproduces the BigModel 1213 error
// by simulating multiple requests where the body becomes corrupted between calls.
func TestRepro_BigModel1213_BodyCorruption(t *testing.T) {
	var receivedBodies []string
	var contentLengths []int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, string(body))
		contentLengths = append(contentLengths, r.ContentLength)

		t.Logf("[MOCK] Request #%d: ContentLength=%d, BodyLen=%d, Body: %s",
			len(receivedBodies), r.ContentLength, len(body), truncate(body))

		// Check for empty/malformed body
		if len(body) == 0 || r.ContentLength > 0 && len(body) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		// Check if messages field is present
		var reqData map[string]interface{}
		if err := json.Unmarshal(body, &reqData); err != nil {
			t.Logf("[MOCK] Invalid JSON!")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"Invalid JSON"}}`))
			return
		}

		if _, hasMessages := reqData["messages"]; !hasMessages {
			t.Logf("[MOCK] Missing messages field!")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"未正常接收到prompt参数。"}}`))
			return
		}

		// Success response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"glm-4.7\",\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n"))
		w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello!\"}}\n\n"))
		w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mockServer.Close()

	transformer := NewGLMAnthropicTransformer()

	// Simulate multiple requests (like failover or retry)
	for i := 1; i <= 5; i++ {
		t.Logf("\n=== Request #%d ===", i)

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

		httpReq, err := transformer.PrepareRequest(req, mockServer.URL, "test-key", "glm-4.7")
		if err != nil {
			t.Fatalf("Request #%d: PrepareRequest failed: %v", i, err)
		}

		// CRITICAL: Check if body is valid BEFORE sending
		if httpReq.Body == nil && httpReq.GetBody == nil {
			t.Errorf("Request #%d: BOTH Body and GetBody are nil!", i)
		}

		// Try to read the body to verify it's valid
		var bodyBytes []byte
		if httpReq.GetBody != nil {
			body, err := httpReq.GetBody()
			if err != nil {
				t.Errorf("Request #%d: GetBody() error: %v", i, err)
			} else {
				bodyBytes, _ = io.ReadAll(body)
				body.Close()
			}
		} else if httpReq.Body != nil {
			bodyBytes, _ = io.ReadAll(httpReq.Body)
		}

		t.Logf("Request #%d: Body bytes length: %d", i, len(bodyBytes))

		// Send actual request
		client := &http.Client{}
		resp, err := client.Do(httpReq)
		if err != nil {
			t.Logf("Request #%d: Do error: %v", i, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	t.Logf("\n=== FINAL RESULTS ===")
	for i, body := range receivedBodies {
		t.Logf("Request #%d: ContentLength=%d, BodyLen=%d, Body: %s",
			i+1, contentLengths[i], len(body), truncate([]byte(body)))
	}

	// Check for issues
	if len(receivedBodies) > 0 {
		// Check if body got corrupted
		for i, body := range receivedBodies {
			if len(body) == 0 {
				t.Errorf("Request #%d: EMPTY BODY received!", i+1)
			}
			if contentLengths[i] > 0 && len(body) == 0 {
				t.Errorf("Request #%d: ContentLength=%d but Body length=0!", i+1, contentLengths[i])
			}
		}
	}
}

// TestRepro_Aliyun_BodyConsumedOnRetry reproduces the Aliyun body length 0 error
// by simulating what happens when HTTP client retries with consumed body.
func TestRepro_Aliyun_BodyConsumedOnRetry(t *testing.T) {
	firstAttempt := true
	firstBodyWasEmpty := false

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		t.Logf("[MOCK] Request received, ContentLength=%d, ActualBodyLen=%d",
			r.ContentLength, len(body))

		// On first attempt, simulate network error that triggers retry
		// and the body is consumed but not properly restored
		if firstAttempt && !firstBodyWasEmpty {
			// Simulate the body being consumed and not restored
			firstBodyWasEmpty = len(body) == 0 && r.ContentLength > 0
			firstAttempt = false
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"retry needed"}`))
			return
		}

		// Check for the body length mismatch
		if r.ContentLength > 0 && len(body) == 0 {
			t.Logf("[MOCK] ERROR: ContentLength=%d but body is empty!", r.ContentLength)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"body length mismatch"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer mockServer.Close()

	transformer := NewGLMAnthropicTransformer()

	req := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Hello"},
				},
			},
		},
	}

	httpReq, err := transformer.PrepareRequest(req, mockServer.URL, "test-key", "glm-4.7")
	if err != nil {
		t.Fatalf("PrepareRequest failed: %v", err)
	}

	// Manually test what happens when body is consumed
	// This simulates what happens in retry scenarios
	if httpReq.GetBody == nil {
		t.Logf("[TEST] GetBody is nil - body cannot be reused for retries!")
	} else {
		t.Logf("[TEST] GetBody is set - body can be reused")
	}

	// Read and consume the body (like a retry would)
	if httpReq.GetBody != nil {
		body1, _ := httpReq.GetBody()
		io.Copy(io.Discard, body1)
		body1.Close()

		// Now try to get body again (simulating retry)
		body2, err := httpReq.GetBody()
		if err != nil {
			t.Errorf("Second GetBody() failed: %v", err)
		} else {
			body2Bytes, _ := io.ReadAll(body2)
			body2.Close()
			t.Logf("[TEST] After first read, second GetBody returns %d bytes", len(body2Bytes))

			if len(body2Bytes) == 0 && httpReq.ContentLength > 0 {
				t.Errorf("BUG REPRODUCED: Body was consumed and GetBody returned empty on retry!")
			}
		}
	}
}

// TestRepro_OpenRouter_EmptyTextBlock reproduces the OpenRouter empty text content error
// by checking if normalization properly handles edge cases.
func TestRepro_OpenRouter_EmptyTextBlock(t *testing.T) {
	testCases := []struct {
		name     string
		messages []anthropic.Message
	}{
		{
			name: "single text block with empty string",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "text", Text: ""}, // Empty string!
					},
				},
			},
		},
		{
			name: "single thinking block - should be normalized",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "some thoughts"},
					},
				},
			},
		},
		{
			name: "assistant message from previous thinking-only response",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "text", Text: "Hello"},
					},
				},
				{
					Role: "assistant",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "I'll help you", Signature: testStrPtr("")},
					},
				},
			},
		},
		{
			name: "mixed content with thinking and text",
			messages: []anthropic.Message{
				{
					Role: "user",
					Content: anthropic.MessageContent{
						{Type: "thinking", Thinking: "thinking"},
						{Type: "text", Text: " "}, // Single space from normalization
					},
				},
			},
		},
	}

	transformer := NewOpenRouterTransformer()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     "anthropic/claude-sonnet-4.5",
				MaxTokens: 1024,
				Messages:  tc.messages,
			}

			// Prepare the request (this applies normalization)
			httpReq, err := transformer.PrepareRequest(req, "http://test.example.com", "test-key", "anthropic/claude-sonnet-4.5")
			if err != nil {
				t.Fatalf("PrepareRequest failed: %v", err)
			}

			// Get the body and check for empty text
			if httpReq.GetBody != nil {
				body, _ := httpReq.GetBody()
				bodyBytes, _ := io.ReadAll(body)
				body.Close()

				// Parse the JSON
				var reqData map[string]interface{}
				if err := json.Unmarshal(bodyBytes, &reqData); err != nil {
					t.Fatalf("Failed to parse request JSON: %v", err)
				}

				// Check each message for empty text blocks
				if messages, ok := reqData["messages"].([]interface{}); ok {
					for i, msg := range messages {
						msgMap := msg.(map[string]interface{})
						content := msgMap["content"]

						t.Logf("Message[%d] content: %+v", i, content)

						// Check for empty text in content
						if contentArr, ok := content.([]interface{}); ok {
							for j, block := range contentArr {
								blockMap := block.(map[string]interface{})
								if blockMap["type"] == "text" {
									text, _ := blockMap["text"].(string)
									if text == "" {
										t.Errorf("FOUND BUG: Message[%d].Content[%d] has EMPTY text string!", i, j)
									} else if text == " " {
										t.Logf("OK: Message[%d].Content[%d] has single space", i, j)
									}
								}
							}
						}
					}
				}
			}
		})
	}
}

// TestRepro_FullFlow_MultipleRequests simulates the actual flow with conversation history
// to see where the corruption happens.
func TestRepro_FullFlow_MultipleRequests(t *testing.T) {
	// This test simulates what happens when:
	// 1. First request succeeds
	// 2. Response is added to conversation history
	// 3. Second request uses conversation history
	// 4. Something goes wrong

	var requestCount int

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, _ := io.ReadAll(r.Body)

		t.Logf("\n[MOCK] Request #%d:", requestCount)
		t.Logf("  Content-Length: %d", r.ContentLength)
		t.Logf("  Body length: %d", len(body))
		t.Logf("  Body: %s", truncate(body))

		if len(body) == 0 {
			t.Errorf("BUG: Request #%d has EMPTY body!", requestCount)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"1213","message":"empty body"}}`))
			return
		}

		// Return a response that includes conversation history
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Include a response that has thinking (to test normalization)
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Response\"}]}}\n\n"))
		w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Response\"}}\n\n"))
		w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":10}}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer mockServer.Close()

	transformer := NewGLMAnthropicTransformer()

	// First request - simple
	req1 := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "First question"},
				},
			},
		},
	}

	// Send first request
	httpReq1, _ := transformer.PrepareRequest(req1, mockServer.URL, "key", "glm-4.7")
	client := &http.Client{}
	resp1, _ := client.Do(httpReq1)
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	// Create second request with conversation history (simulating what Claude Code does)
	// In real scenario, the response would be added to messages
	req2 := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "First question"},
				},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "First response"},
				},
			},
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Second question"},
				},
			},
		},
	}

	// Send second request
	httpReq2, _ := transformer.PrepareRequest(req2, mockServer.URL, "key", "glm-4.7")
	resp2, _ := client.Do(httpReq2)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	// Third request - with thinking content (simulating conversation with thinking)
	req3 := &anthropic.Request{
		Model:     "glm-4.7",
		MaxTokens: 1024,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "First question"},
				},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					{Type: "thinking", Thinking: "Let me think about this", Signature: testStrPtr("sig")},
					{Type: "text", Text: "Response"},
				},
			},
			{
				Role: "user",
				Content: anthropic.MessageContent{
					{Type: "text", Text: "Follow up"},
				},
			},
		},
	}

	httpReq3, _ := transformer.PrepareRequest(req3, mockServer.URL, "key", "glm-4.7")
	resp3, _ := client.Do(httpReq3)
	io.Copy(io.Discard, resp3.Body)
	resp3.Body.Close()

	t.Logf("\n=== Total requests sent: %d ===", requestCount)
}

// Helper function
func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}