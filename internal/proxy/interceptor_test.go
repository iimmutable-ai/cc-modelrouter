// Package proxy tests for interceptor functionality.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// Test interceptors

type mockRequestInterceptor struct {
	called bool
	err    error
}

func (m *mockRequestInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	m.called = true
	return m.err
}

type mockResponseInterceptor struct {
	called bool
	err    error
}

func (m *mockResponseInterceptor) InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error {
	m.called = true
	return m.err
}

type mockStreamingInterceptor struct {
	called bool
	err    error
	data   []byte
}

func (m *mockStreamingInterceptor) InterceptStreamingEvent(ctx context.Context, req *anthropic.Request, eventType string, data []byte) ([]byte, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	if m.data != nil {
		return m.data, nil
	}
	return data, nil
}

func TestHandler_AddRequestInterceptor(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	interceptor := &mockRequestInterceptor{}
	handler.AddRequestInterceptor(interceptor)

	if len(handler.requestInterceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(handler.requestInterceptors))
	}
}

func TestHandler_AddResponseInterceptor(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	interceptor := &mockResponseInterceptor{}
	handler.AddResponseInterceptor(interceptor)

	if len(handler.responseInterceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(handler.responseInterceptors))
	}
}

func TestHandler_AddStreamingInterceptor(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	interceptor := &mockStreamingInterceptor{}
	handler.AddStreamingInterceptor(interceptor)

	if len(handler.streamingInterceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(handler.streamingInterceptors))
	}
}

func TestHandler_SetRequestInterceptors(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	interceptors := []RequestInterceptor{
		&mockRequestInterceptor{},
		&mockRequestInterceptor{},
	}
	handler.SetRequestInterceptors(interceptors)

	if len(handler.requestInterceptors) != 2 {
		t.Errorf("expected 2 interceptors, got %d", len(handler.requestInterceptors))
	}
}

func TestHandler_RequestInterceptorCalled(t *testing.T) {
	handler := NewHandler(1024 * 1024)
	handler.SetRouter(&mockRouter{})
	handler.SetTransformerRegistry(&mockTransformerRegistry{})

	interceptor := &mockRequestInterceptor{}
	handler.AddRequestInterceptor(interceptor)

	mockClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			resp := &anthropic.Response{
				ID:      "msg_123",
				Type:    "message",
				Content: []anthropic.ContentBlock{{Type: "text", Text: "test"}},
			}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}
	handler.SetProviderClients(map[string]HTTPClient{"anthropic": mockClient})
	handler.SetConfig(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL:     "https://api.anthropic.com",
				APIKey:      "test-key",
				Transformer: "anthropic",
			},
		},
	})

	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hello"}]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	anthropicReq := &anthropic.Request{}
	json.Unmarshal([]byte(reqBody), anthropicReq)
	handler.handleMessages(w, req, anthropicReq, nil)

	if !interceptor.called {
		t.Error("expected request interceptor to be called")
	}
}

func TestHandler_ResponseInterceptorCalled(t *testing.T) {
	handler := NewHandler(1024 * 1024)
	handler.SetRouter(&mockRouter{})
	handler.SetTransformerRegistry(&mockTransformerRegistry{})

	interceptor := &mockResponseInterceptor{}
	handler.AddResponseInterceptor(interceptor)

	mockClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			resp := &anthropic.Response{
				ID:      "msg_123",
				Type:    "message",
				Content: []anthropic.ContentBlock{{Type: "text", Text: "test"}},
			}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}
	handler.SetProviderClients(map[string]HTTPClient{"anthropic": mockClient})
	handler.SetConfig(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL:     "https://api.anthropic.com",
				APIKey:      "test-key",
				Transformer: "anthropic",
			},
		},
	})

	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hello"}]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	anthropicReq := &anthropic.Request{}
	json.Unmarshal([]byte(reqBody), anthropicReq)
	handler.handleMessages(w, req, anthropicReq, nil)

	if !interceptor.called {
		t.Error("expected response interceptor to be called")
	}
}

func TestHandler_RequestInterceptorError(t *testing.T) {
	handler := NewHandler(1024 * 1024)
	handler.SetRouter(&mockRouter{})
	handler.SetTransformerRegistry(&mockTransformerRegistry{})

	interceptor := &mockRequestInterceptor{
		err: errors.New("interceptor error"),
	}
	handler.AddRequestInterceptor(interceptor)

	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hello"}]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	anthropicReq := &anthropic.Request{}
	json.Unmarshal([]byte(reqBody), anthropicReq)
	handler.handleMessages(w, req, anthropicReq, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Request interceptor error") {
		t.Error("expected interceptor error in response")
	}
}

func TestHandler_ResponseInterceptorError(t *testing.T) {
	handler := NewHandler(1024 * 1024)
	handler.SetRouter(&mockRouter{})
	handler.SetTransformerRegistry(&mockTransformerRegistry{})

	interceptor := &mockResponseInterceptor{
		err: errors.New("interceptor error"),
	}
	handler.AddResponseInterceptor(interceptor)

	mockClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			resp := &anthropic.Response{
				ID:      "msg_123",
				Type:    "message",
				Content: []anthropic.ContentBlock{{Type: "text", Text: "test"}},
			}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}
	handler.SetProviderClients(map[string]HTTPClient{"anthropic": mockClient})
	handler.SetConfig(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL:     "https://api.anthropic.com",
				APIKey:      "test-key",
				Transformer: "anthropic",
			},
		},
	})

	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": [{"type": "text", "text", "text": "hello"}]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	anthropicReq := &anthropic.Request{}
	json.Unmarshal([]byte(reqBody), anthropicReq)
	handler.handleMessages(w, req, anthropicReq, nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestNewLoggingInterceptor(t *testing.T) {
	interceptor := NewLoggingInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if !interceptor.LogRequestDetails {
		t.Error("expected LogRequestDetails to be true")
	}
	if !interceptor.LogResponseDetails {
		t.Error("expected LogResponseDetails to be true")
	}
}

func TestLoggingInterceptor_InterceptRequest(t *testing.T) {
	interceptor := NewLoggingInterceptor()

	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 1024,
		Stream:    false,
		Messages: []anthropic.Message{
			{Content: []anthropic.ContentBlock{{Type: "text", Text: "hello"}}},
		},
	}

	err := interceptor.InterceptRequest(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestLoggingInterceptor_InterceptResponse(t *testing.T) {
	interceptor := NewLoggingInterceptor()

	req := &anthropic.Request{}
	resp := &anthropic.Response{
		ID:         "msg_123",
		Model:      "claude-3-5-sonnet",
		Type:       "message",
		StopReason: "end_turn",
		Usage: anthropic.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	err := interceptor.InterceptResponse(context.Background(), req, resp)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestNewMetricsInterceptor(t *testing.T) {
	interceptor := NewMetricsInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.RequestCount != 0 {
		t.Errorf("expected RequestCount 0, got %d", interceptor.RequestCount)
	}
}

func TestMetricsInterceptor_Tracking(t *testing.T) {
	interceptor := NewMetricsInterceptor()

	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 1024,
	}

	// Intercept request
	interceptor.InterceptRequest(context.Background(), req)
	if interceptor.RequestCount != 1 {
		t.Errorf("expected RequestCount 1, got %d", interceptor.RequestCount)
	}

	// Intercept response
	resp := &anthropic.Response{
		ID:    "msg_123",
		Type:  "message",
		Usage: anthropic.Usage{InputTokens: 100, OutputTokens: 50},
	}
	interceptor.InterceptResponse(context.Background(), req, resp)
	if interceptor.ResponseCount != 1 {
		t.Errorf("expected ResponseCount 1, got %d", interceptor.ResponseCount)
	}

	// Check metrics
	reqCount, respCount, errCount, uptime := interceptor.GetMetrics()
	if reqCount != 1 {
		t.Errorf("expected request count 1, got %d", reqCount)
	}
	if respCount != 1 {
		t.Errorf("expected response count 1, got %d", respCount)
	}
	if errCount != 0 {
		t.Errorf("expected error count 0, got %d", errCount)
	}
	if uptime < 0 {
		t.Errorf("expected non-negative uptime, got %v", uptime)
	}
}

func TestNewTimingInterceptor(t *testing.T) {
	interceptor := NewTimingInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.requestStarts == nil {
		t.Error("expected requestStarts map to be initialized")
	}
}

func TestTimingInterceptor_Timing(t *testing.T) {
	interceptor := NewTimingInterceptor()

	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 1024,
	}

	// Intercept request
	interceptor.InterceptRequest(context.Background(), req)

	// Intercept response
	resp := &anthropic.Response{
		ID:    "msg_123",
		Type:  "message",
		Usage: anthropic.Usage{InputTokens: 100, OutputTokens: 50},
	}
	interceptor.InterceptResponse(context.Background(), req, resp)

	// Request should be removed from map after completion
	if _, ok := interceptor.requestStarts[req]; ok {
		t.Error("expected request to be removed from map after completion")
	}
}

func TestServer_AddInterceptorMethods(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 8081,
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Add request interceptor
	server.AddRequestInterceptor(&mockRequestInterceptor{})
	// Add response interceptor
	server.AddResponseInterceptor(&mockResponseInterceptor{})
	// Add streaming interceptor
	server.AddStreamingInterceptor(&mockStreamingInterceptor{})

	if len(server.handler.requestInterceptors) != 1 {
		t.Errorf("expected 1 request interceptor, got %d", len(server.handler.requestInterceptors))
	}
	if len(server.handler.responseInterceptors) != 1 {
		t.Errorf("expected 1 response interceptor, got %d", len(server.handler.responseInterceptors))
	}
	if len(server.handler.streamingInterceptors) != 1 {
		t.Errorf("expected 1 streaming interceptor, got %d", len(server.handler.streamingInterceptors))
	}
}

func TestHandler_SendStreamingError(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	w := httptest.NewRecorder()
	err := errors.New("test error")

	handler.sendStreamingError(w, "Test message", err)

	output := w.Body.String()

	if !strings.Contains(output, "event: error") {
		t.Error("expected 'event: error' in output")
	}
	if !strings.Contains(output, "Test message") {
		t.Error("expected 'Test message' in output")
	}
	if !strings.Contains(output, "api_error") {
		t.Error("expected 'api_error' in output")
	}
}

func TestHandler_SendStreamingError_Format(t *testing.T) {
	handler := NewHandler(1024 * 1024)

	w := httptest.NewRecorder()
	err := errors.New("provider failed")

	handler.sendStreamingError(w, "All providers failed", err)

	output := w.Body.String()

	// Check Anthropic's SSE error format
	expectedParts := []string{
		"event: error",
		"data: ",
		`"type": "error"`,
		`"error": {`,
		`"type": "api_error"`,
		`"message": "All providers failed: provider failed"`,
	}

	for _, part := range expectedParts {
		if !strings.Contains(output, part) {
			t.Errorf("expected '%s' in output, got: %s", part, output)
		}
	}
}

// streamingTransformer is a mock transformer that supports streaming
// streamingTransformer is a mock transformer that supports streaming
type streamingTransformer struct{}

func (m *streamingTransformer) Name() string { return "streaming" }
func (m *streamingTransformer) Endpoint() string { return "/v1/chat/completions" }
func (m *streamingTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	return nil, nil
}
func (m *streamingTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	return &anthropic.Response{}, nil
}
func (m *streamingTransformer) SupportsStreaming() bool {
	return true
}
func (m *streamingTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	return []transformer.SSEEvent{*event}, nil
}

func TestHandler_StreamingInterceptorCalled(t *testing.T) {
	handler := NewHandler(1024 * 1024)
	handler.SetRouter(&mockRouter{})

	reg := &mockTransformerRegistry{}
	reg.addTransformer("streaming", &streamingTransformer{})
	handler.SetTransformerRegistry(reg)

	interceptor := &mockStreamingInterceptor{}
	handler.AddStreamingInterceptor(interceptor)

	// Create a streaming SSE response
	sseResponse := `event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

event: message_stop
data: {"type":"message_stop","stop_reason":"end_turn"}

`

	mockClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       httptest.NewRequest("GET", "/", strings.NewReader(sseResponse)).Body,
			}, nil
		},
	}
	handler.SetProviderClients(map[string]HTTPClient{"anthropic": mockClient})
	handler.SetConfig(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL:     "https://api.anthropic.com",
				APIKey:      "test-key",
				Transformer: "streaming",
			},
		},
	})

	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"stream": true,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hello"}]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	anthropicReq := &anthropic.Request{}
	json.Unmarshal([]byte(reqBody), anthropicReq)

	handler.handleStreaming(w, req, anthropicReq, "test-route", []config.RouteTarget{{Provider: "anthropic", Model: "claude-3"}}, nil)

	if !interceptor.called {
		t.Error("expected streaming interceptor to be called")
	}
}
