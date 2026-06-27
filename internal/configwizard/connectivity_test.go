package configwizard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestParseAnthropicResponse(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantInput     int
		wantOutput    int
		wantCostEmpty bool
	}{
		{
			name:       "valid response",
			body:       `{"usage":{"input_tokens":100,"output_tokens":50}}`,
			wantInput:  100,
			wantOutput: 50,
		},
		{
			name:          "empty usage",
			body:          `{"usage":{"input_tokens":0,"output_tokens":0}}`,
			wantInput:     0,
			wantOutput:    0,
			wantCostEmpty: true,
		},
		{
			name:          "no usage field",
			body:          `{"id":"msg_123"}`,
			wantInput:     0,
			wantOutput:    0,
			wantCostEmpty: true,
		},
		{
			name:          "invalid json",
			body:          `{not json}`,
			wantInput:     0,
			wantOutput:    0,
			wantCostEmpty: false, // calculateCost not called when unmarshal fails
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TestConnectionResult{}
			r.parseAnthropicResponse([]byte(tt.body))
			if r.InputTokens != tt.wantInput {
				t.Errorf("InputTokens = %d, want %d", r.InputTokens, tt.wantInput)
			}
			if r.OutputTokens != tt.wantOutput {
				t.Errorf("OutputTokens = %d, want %d", r.OutputTokens, tt.wantOutput)
			}
			if tt.wantCostEmpty && r.CostEstimate != "$0.0000" {
				t.Errorf("CostEstimate = %s, want $0.0000", r.CostEstimate)
			}
		})
	}
}

func TestParseGLMResponse(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantInput  int
		wantOutput int
	}{
		{
			name:       "valid response",
			body:       `{"usage":{"prompt_tokens":200,"completion_tokens":80,"total_tokens":280}}`,
			wantInput:  200,
			wantOutput: 80,
		},
		{
			name:       "no usage",
			body:       `{"id":"chatcmpl-123"}`,
			wantInput:  0,
			wantOutput: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TestConnectionResult{}
			r.parseGLMResponse([]byte(tt.body))
			if r.InputTokens != tt.wantInput {
				t.Errorf("InputTokens = %d, want %d", r.InputTokens, tt.wantInput)
			}
			if r.OutputTokens != tt.wantOutput {
				t.Errorf("OutputTokens = %d, want %d", r.OutputTokens, tt.wantOutput)
			}
		})
	}
}

func TestParseGeminiResponse(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantInput  int
		wantOutput int
	}{
		{
			name:       "valid response",
			body:       `{"usageMetadata":{"promptTokenCount":150,"candidatesTokenCount":60,"totalTokenCount":210}}`,
			wantInput:  150,
			wantOutput: 60,
		},
		{
			name:       "no usage",
			body:       `{"candidates":[{}]}`,
			wantInput:  0,
			wantOutput: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TestConnectionResult{}
			r.parseGeminiResponse([]byte(tt.body))
			if r.InputTokens != tt.wantInput {
				t.Errorf("InputTokens = %d, want %d", r.InputTokens, tt.wantInput)
			}
			if r.OutputTokens != tt.wantOutput {
				t.Errorf("OutputTokens = %d, want %d", r.OutputTokens, tt.wantOutput)
			}
		})
	}
}

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		output   int
		wantCost string
	}{
		{"zero tokens", 0, 0, "$0.0000"},
		{"input only", 1000, 0, "$0.0001"},
		{"output only", 0, 1000, "$0.0005"},
		{"both", 1000, 1000, "$0.0006"},
		{"large", 100000, 50000, "$0.0350"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TestConnectionResult{InputTokens: tt.input, OutputTokens: tt.output}
			r.calculateCost()
			if r.CostEstimate != tt.wantCost {
				t.Errorf("CostEstimate = %s, want %s", r.CostEstimate, tt.wantCost)
			}
		})
	}
}

func TestBuildAnthropicTestRequest(t *testing.T) {
	req, err := buildAnthropicTestRequest("https://api.example.com/v1/messages", "sk-test", "claude-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("Method = %s, want POST", req.Method)
	}
	if req.Header.Get("x-api-key") != "sk-test" {
		t.Errorf("x-api-key = %s, want sk-test", req.Header.Get("x-api-key"))
	}
	if req.Header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %s", req.Header.Get("anthropic-version"))
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s", req.Header.Get("Content-Type"))
	}

	var body map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["model"] != "claude-3" {
		t.Errorf("model = %v, want claude-3", body["model"])
	}
}

func TestBuildOpenRouterTestRequest(t *testing.T) {
	req, err := buildOpenRouterTestRequest("https://openrouter.ai/api/v1/chat/completions", "sk-or", "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("Method = %s, want POST", req.Method)
	}
	if req.Header.Get("Authorization") != "Bearer sk-or" {
		t.Errorf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestBuildGLMTestRequest(t *testing.T) {
	req, err := buildGLMTestRequest("https://open.bigmodel.cn/api/paic/v1/chat/completions", "sk-glm", "glm-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("Method = %s, want POST", req.Method)
	}
	if req.Header.Get("Authorization") != "Bearer sk-glm" {
		t.Errorf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestBuildGeminiTestRequest(t *testing.T) {
	req, err := buildGeminiTestRequest("https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent", "sk-gem", "gemini-pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("Method = %s, want POST", req.Method)
	}
	if req.Header.Get("Authorization") != "Bearer sk-gem" {
		t.Errorf("Authorization = %s", req.Header.Get("Authorization"))
	}

	var body map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := body["contents"]; !ok {
		t.Error("body missing 'contents' field")
	}
	if _, ok := body["generationConfig"]; !ok {
		t.Error("body missing 'generationConfig' field")
	}
}

func TestTestProviderConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"usage": map[string]int{
					"input_tokens":  10,
					"output_tokens": 5,
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := TestProviderConnection("anthropic", config.ProviderConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}, "claude-3")

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.InputTokens)
	}
	if result.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", result.OutputTokens)
	}
	if result.Latency <= 0 {
		t.Error("Latency should be positive")
	}
}

func TestTestProviderConnection_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "invalid api key",
			},
		})
	}))
	defer server.Close()

	result := TestProviderConnection("anthropic", config.ProviderConfig{
		BaseURL: server.URL,
		APIKey:  "bad-key",
	}, "claude-3")

	if result.Success {
		t.Error("expected failure for 401 response")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestTestProviderConnection_ConnectionRefused(t *testing.T) {
	result := TestProviderConnection("anthropic", config.ProviderConfig{
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "test-key",
	}, "claude-3")

	if result.Success {
		t.Error("expected failure for connection refused")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestTestProviderConnection_GLMParse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"usage": map[string]int{
				"prompt_tokens":     30,
				"completion_tokens": 10,
				"total_tokens":      40,
			},
		})
	}))
	defer server.Close()

	result := TestProviderConnection("bigmodel", config.ProviderConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}, "glm-4")

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", result.InputTokens)
	}
	if result.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", result.OutputTokens)
	}
}

func TestTestProviderConnection_GeminiParse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"usageMetadata": map[string]int{
				"promptTokenCount":     20,
				"candidatesTokenCount": 8,
				"totalTokenCount":      28,
			},
		})
	}))
	defer server.Close()

	result := TestProviderConnection("gemini", config.ProviderConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}, "gemini-pro")

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", result.InputTokens)
	}
	if result.OutputTokens != 8 {
		t.Errorf("OutputTokens = %d, want 8", result.OutputTokens)
	}
}
