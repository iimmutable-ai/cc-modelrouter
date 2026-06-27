//go:build integration
// +build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/cli"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
)

// TestIntegrationStreamingSSE tests that the streaming handler properly
// forwards SSE events from the provider. The provider must send the complete
// event stream including message_start and content_block_start.
func TestIntegrationStreamingSSE(t *testing.T) {
	// Create a mock provider server that returns streaming responses
	mockProviderCalled := false
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockProviderCalled = true

		// Verify the request was properly formatted
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Streaming not supported")
		}

		// Send message_start event (provider must send this)
		messageStartData := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            "msg_test123",
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         "test-model",
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  1,
					"output_tokens": 0,
				},
			},
		}
		messageStartJSON, _ := json.Marshal(messageStartData)
		w.Write([]byte("event: message_start\n"))
		w.Write([]byte("data: "))
		w.Write(messageStartJSON)
		w.Write([]byte("\n\n"))
		flusher.Flush()

		// Send content_block_start event (provider must send this)
		contentBlockStartData := map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		}
		contentBlockStartJSON, _ := json.Marshal(contentBlockStartData)
		w.Write([]byte("event: content_block_start\n"))
		w.Write([]byte("data: "))
		w.Write(contentBlockStartJSON)
		w.Write([]byte("\n\n"))
		flusher.Flush()

		// Send a simple text delta
		deltaData := map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]string{
				"type": "text_delta",
				"text": "Hello",
			},
		}
		deltaJSON, _ := json.Marshal(deltaData)
		w.Write([]byte("event: content_block_delta\n"))
		w.Write([]byte("data: "))
		w.Write(deltaJSON)
		w.Write([]byte("\n\n"))
		flusher.Flush()

		// Send content_block_stop
		stopData := map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		}
		stopJSON, _ := json.Marshal(stopData)
		w.Write([]byte("event: content_block_stop\n"))
		w.Write([]byte("data: "))
		w.Write(stopJSON)
		w.Write([]byte("\n\n"))
		flusher.Flush()

		// Send message_stop
		messageStopData := map[string]string{
			"type": "message_stop",
		}
		messageStopJSON, _ := json.Marshal(messageStopData)
		w.Write([]byte("event: message_stop\n"))
		w.Write([]byte("data: "))
		w.Write(messageStopJSON)
		w.Write([]byte("\n\n"))
		flusher.Flush()
	}))
	defer mockProvider.Close()

	// Create test configuration
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"test": {
				BaseURL:     mockProvider.URL,
				APIKey:      "test-key",
				Transformer: "anthropic",
				Models:      []string{"test-model"},
			},
		},
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "test:test-model",
			},
			MaxRetries:  3,
			RetryDelay: "1s",
		},
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	// Initialize components
	registry := transformer.NewRegistry()
	registry.Register(transformers.NewAnthropicTransformer())

	// Create HTTP client
	clients := map[string]proxy.HTTPClient{
		"test": &http.Client{Timeout: 30 * time.Second},
	}

	routerEngine := router.NewEngine(cfg)

	// Create handler
	handler := proxy.NewHandler(50 * 1024 * 1024)
	handler.SetRouter(cli.NewRouterAdapter(routerEngine))
	handler.SetTransformerRegistry(cli.NewRegistryAdapter(registry))
	handler.SetProviderClients(clients)
	handler.SetConfig(cfg)
	handler.SetInstanceID("test-inst-sse")

	// Create streaming request
	reqBody := map[string]any{
		"model":      "test-model",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'Hello'"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify the mock provider was called
	if !mockProviderCalled {
		t.Error("Mock provider was not called")
	}

	// Check response
	if w.Code != http.StatusOK {
		t.Logf("Response: %s", w.Body.String())
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify SSE headers
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Parse the SSE response
	responseBody := w.Body.String()
	t.Logf("SSE Response:\n%s", responseBody)

	// Verify required events are present
	events := parseSSEEvents(t, responseBody)

	// Check for message_start event
	if !hasEvent(events, "message_start") {
		t.Error("Missing required 'message_start' event")
	}
	// Check for content_block_start event
	if !hasEvent(events, "content_block_start") {
		t.Error("Missing required 'content_block_start' event")
	}
	// Check for content_block_delta event
	if !hasEvent(events, "content_block_delta") {
		t.Error("Missing 'content_block_delta' event")
	}
	// Check for content_block_stop event
	if !hasEvent(events, "content_block_stop") {
		t.Error("Missing 'content_block_stop' event")
	}
	// Check for message_stop event
	if !hasEvent(events, "message_stop") {
		t.Error("Missing 'message_stop' event")
	}

	// Verify event order
	eventOrder := getEventOrder(events)
	expectedOrder := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_stop"}
	if !eventOrdersMatch(eventOrder, expectedOrder) {
		t.Errorf("Event order mismatch. Got %v, want %v", eventOrder, expectedOrder)
	}

	// Verify message_start event contains required fields
	messageStartEvent := getEventByType(events, "message_start")
	if messageStartEvent != nil {
		if !strings.Contains(string(messageStartEvent.data), `"type":"message_start"`) {
			t.Error("message_start event missing type field")
		}
		if !strings.Contains(string(messageStartEvent.data), `"message":`) {
			t.Error("message_start event missing message field")
		}
		if !strings.Contains(string(messageStartEvent.data), `"id":`) {
			t.Error("message_start event missing id field")
		}
	}

	// Verify content_block_start event contains required fields
	contentBlockStartEvent := getEventByType(events, "content_block_start")
	if contentBlockStartEvent != nil {
		if !strings.Contains(string(contentBlockStartEvent.data), `"type":"content_block_start"`) {
			t.Error("content_block_start event missing type field")
		}
		if !strings.Contains(string(contentBlockStartEvent.data), `"index":0`) {
			t.Error("content_block_start event missing index field")
		}
	}

	// Verify content_block_delta event contains the text content
	contentBlockDeltaEvent := getEventByType(events, "content_block_delta")
	if contentBlockDeltaEvent != nil {
		if !strings.Contains(string(contentBlockDeltaEvent.data), `"text":"Hello"`) {
			t.Errorf("content_block_delta event missing expected text. Got: %s", string(contentBlockDeltaEvent.data))
		}
	}
}

// sseEvent represents a parsed SSE event
type sseEvent struct {
	eventType string
	data      []byte
}

// parseSSEEvents parses SSE events from a response body
func parseSSEEvents(t *testing.T, body string) []*sseEvent {
	var events []*sseEvent
	lines := strings.Split(body, "\n")

	var currentEvent *sseEvent
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if currentEvent != nil {
				events = append(events, currentEvent)
				currentEvent = nil
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			if currentEvent == nil {
				currentEvent = &sseEvent{}
			}
			currentEvent.eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			if currentEvent == nil {
				currentEvent = &sseEvent{}
			}
			currentEvent.data = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	// Don't forget the last event
	if currentEvent != nil {
		events = append(events, currentEvent)
	}

	return events
}

// hasEvent checks if an event with the given type exists
func hasEvent(events []*sseEvent, eventType string) bool {
	for _, e := range events {
		if e.eventType == eventType {
			return true
		}
	}
	return false
}

// getEventOrder returns the event types in order
func getEventOrder(events []*sseEvent) []string {
	var order []string
	for _, e := range events {
		order = append(order, e.eventType)
	}
	return order
}

// eventOrdersMatch checks if the event orders match
func eventOrdersMatch(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}

// getEventByType returns the first event with the given type
func getEventByType(events []*sseEvent, eventType string) *sseEvent {
	for _, e := range events {
		if e.eventType == eventType {
			return e
		}
	}
	return nil
}
