package configwizard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// TestConnectionResult contains the result of a connection test.
type TestConnectionResult struct {
	Success   bool
	Latency   time.Duration
	Error     string
	InputTokens  int
	OutputTokens int
	CostEstimate string
}

// TestProviderConnection tests connectivity to a provider with a specific model.
func TestProviderConnection(providerName string, providerCfg config.ProviderConfig, model string) *TestConnectionResult {
	result := &TestConnectionResult{}

	// Resolve ${VAR} references in API key for the actual connection test
	apiKey := os.ExpandEnv(providerCfg.APIKey)

	// Build the request based on the provider
	var req *http.Request
	var url string
	var err error

	switch providerName {
	case "anthropic":
		url = fmt.Sprintf("%s/v1/messages", strings.TrimSuffix(providerCfg.BaseURL, "/"))
		req, err = buildAnthropicTestRequest(url, apiKey, model)
	case "openrouter", "openrouter-openai", "openrouter-anthropic":
		url = "https://openrouter.ai/api/v1/chat/completions"
		req, err = buildOpenRouterTestRequest(url, apiKey, model)
	case "bigmodel":
		url = fmt.Sprintf("%s/api/paic/v1/chat/completions", strings.TrimSuffix(providerCfg.BaseURL, "/"))
		req, err = buildGLMTestRequest(url, apiKey, model)
	case "gemini":
		url = fmt.Sprintf("%s/v1beta/models/%s:generateContent", strings.TrimSuffix(providerCfg.BaseURL, "/"), model)
		req, err = buildGeminiTestRequest(url, apiKey, model)
	default:
		// Generic test - just try to reach the base URL
		url = providerCfg.BaseURL
		req, err = http.NewRequest("GET", url, nil)
		if err == nil {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		}
	}

	if err != nil {
		result.Error = fmt.Sprintf("Failed to build request: %v", err)
		return result
	}

	// Make the request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTime := time.Now()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	result.Latency = time.Since(startTime)

	if err != nil {
		result.Error = fmt.Sprintf("Connection failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to read response: %v", err)
		return result
	}

	// Check status code
	if resp.StatusCode >= 400 {
		// Try to extract error message from response
		var errBody map[string]interface{}
		if json.Unmarshal(body, &errBody) != nil {
			result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		} else if msg, ok := errBody["error"].(string); ok {
			result.Error = msg
		} else if errObj, ok := errBody["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok {
				result.Error = msg
			} else {
				result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
		} else {
			result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return result
	}

	result.Success = true

	// Try to extract usage info from response
	switch providerName {
	case "anthropic", "openrouter", "openrouter-openai", "openrouter-anthropic":
		result.parseAnthropicResponse(body)
	case "bigmodel":
		result.parseGLMResponse(body)
	case "gemini":
		result.parseGeminiResponse(body)
	}

	return result
}

func (r *TestConnectionResult) parseAnthropicResponse(body []byte) {
	var resp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) == nil {
		r.InputTokens = resp.Usage.InputTokens
		r.OutputTokens = resp.Usage.OutputTokens
		r.calculateCost()
	}
}

func (r *TestConnectionResult) parseGLMResponse(body []byte) {
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) == nil {
		r.InputTokens = resp.Usage.PromptTokens
		r.OutputTokens = resp.Usage.CompletionTokens
		r.calculateCost()
	}
}

func (r *TestConnectionResult) parseGeminiResponse(body []byte) {
	var resp struct {
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(body, &resp) == nil {
		r.InputTokens = resp.UsageMetadata.PromptTokenCount
		r.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
		r.calculateCost()
	}
}

func (r *TestConnectionResult) calculateCost() {
	// Rough cost estimation (in USD)
	inputCost := float64(r.InputTokens) / 1000 * 0.0001  // $0.10 per 1M tokens
	outputCost := float64(r.OutputTokens) / 1000 * 0.0005 // $0.50 per 1M tokens
	total := inputCost + outputCost
	r.CostEstimate = fmt.Sprintf("$%.4f", total)
}

func buildAnthropicTestRequest(url, apiKey, model string) (*http.Request, error) {
	body := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	return req, nil
}

func buildOpenRouterTestRequest(url, apiKey, model string) (*http.Request, error) {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"max_tokens": 1,
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("HTTP-Referer", "https://github.com/iimmutable-ai/cc-modelrouter")
	req.Header.Set("X-Title", "cc-modelrouter")
	return req, nil
}

func buildGLMTestRequest(url, apiKey, model string) (*http.Request, error) {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"max_tokens": 1,
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	return req, nil
}

func buildGeminiTestRequest(url, apiKey, model string) (*http.Request, error) {
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": "Hi"},
				},
			},
		},
		"generationConfig": map[string]int{
			"maxOutputTokens": 1,
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	return req, nil
}