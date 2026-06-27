// Package interceptor tests for max_token interceptor.
package interceptor

import (
	"context"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestNewMaxTokenInterceptor(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config == nil {
		t.Error("expected config to be initialized")
	}
	if interceptor.config.DefaultLimit == 0 {
		t.Error("expected default limit to be set")
	}
}

func TestNewMaxTokenInterceptorWithConfig(t *testing.T) {
	config := &MaxTokenConfig{
		DefaultLimit:  1000,
		ProviderLimits: map[string]int{
			"test": 500,
		},
		ModelLimits: map[string]int{
			"test-model": 200,
		},
	}

	interceptor := NewMaxTokenInterceptorWithConfig(config)

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config != config {
		t.Error("expected config to be set")
	}
	if interceptor.config.DefaultLimit != 1000 {
		t.Errorf("expected default limit 1000, got %d", interceptor.config.DefaultLimit)
	}
}

func TestDefaultMaxTokenConfig(t *testing.T) {
	config := DefaultMaxTokenConfig()

	if config == nil {
		t.Error("expected non-nil config")
	}
	if config.DefaultLimit == 0 {
		t.Error("expected default limit to be set")
	}

	// Check provider limits
	expectedProviders := []string{"anthropic", "openai", "openrouter", "gemini", "glm", "qwen", "minimax"}
	for _, provider := range expectedProviders {
		if _, ok := config.ProviderLimits[provider]; !ok {
			t.Errorf("expected provider limit for %s", provider)
		}
	}

	// Check model limits
	expectedModels := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-opus-20240229",
		"gpt-4-turbo-preview",
		"gpt-4o",
		"gemini-1.5-pro",
	}
	for _, model := range expectedModels {
		if _, ok := config.ModelLimits[model]; !ok {
			t.Errorf("expected model limit for %s", model)
		}
	}
}

func TestMaxTokenInterceptor_ClaudeModels(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "Claude 3.5 Sonnet",
			model:         "claude-3-5-sonnet-20241022",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
		{
			name:          "Claude 3.5 Haiku",
			model:         "claude-3-5-haiku-20241022",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
		{
			name:          "Claude 3 Opus",
			model:         "claude-3-opus-20240229",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "Claude 3 Sonnet",
			model:         "claude-3-sonnet-20240229",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "Claude 3 Haiku",
			model:         "claude-3-haiku-20240307",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_OpenAIModels(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "GPT-4 Turbo",
			model:         "gpt-4-turbo-preview",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "GPT-4o",
			model:         "gpt-4o",
			maxTokens:     200000,
			expectedLimit: 128000,
		},
		{
			name:          "GPT-4o-mini",
			model:         "gpt-4o-mini",
			maxTokens:     20000,
			expectedLimit: 16000,
		},
		{
			name:          "GPT-3.5 Turbo",
			model:         "gpt-3.5-turbo-0125",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "GPT-3.5 Turbo 16K",
			model:         "gpt-3.5-turbo-16k",
			maxTokens:     20000,
			expectedLimit: 16384,
		},
		{
			name:          "GPT-4 32K",
			model:         "gpt-4-32k",
			maxTokens:     40000,
			expectedLimit: 32768,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_GeminiModels(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "Gemini 1.5 Pro",
			model:         "gemini-1.5-pro",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
		{
			name:          "Gemini 1.5 Flash",
			model:         "gemini-1.5-flash",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
		{
			name:          "Gemini 1.0 Pro",
			model:         "gemini-1.0-pro",
			maxTokens:     3000,
			expectedLimit: 2048,
		},
		{
			name:          "Gemini Pro",
			model:         "gemini-pro",
			maxTokens:     3000,
			expectedLimit: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_NoAdjustment(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name      string
		model     string
		maxTokens int
	}{
		{
			name:      "Within limit",
			model:     "claude-3-5-sonnet-20241022",
			maxTokens: 4000,
		},
		{
			name:      "At limit",
			model:     "claude-3-5-sonnet-20241022",
			maxTokens: 8192,
		},
		{
			name:      "Zero max_tokens",
			model:     "claude-3-5-sonnet-20241022",
			maxTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalTokens := tt.maxTokens
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != originalTokens {
				t.Errorf("expected max_tokens %d (unchanged), got %d", originalTokens, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_UnknownModel(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "Unknown model",
			model:         "unknown-model",
			maxTokens:     10000,
			expectedLimit: 4096, // Default limit
		},
		{
			name:          "Custom provider model",
			model:         "custom-provider/custom-model",
			maxTokens:     10000,
			expectedLimit: 4096, // Default limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_CustomConfig(t *testing.T) {
	config := &MaxTokenConfig{
		DefaultLimit: 500,
		ProviderLimits: map[string]int{
			"custom": 2000,
		},
		ModelLimits: map[string]int{
			"custom-model-v1": 1000,
		},
	}

	interceptor := NewMaxTokenInterceptorWithConfig(config)

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "Custom exact model match",
			model:         "custom-model-v1",
			maxTokens:     5000,
			expectedLimit: 1000,
		},
		{
			name:          "Custom provider limit",
			model:         "custom/some-model",
			maxTokens:     5000,
			expectedLimit: 500, // Fallback to default since no prefix match
		},
		{
			name:          "Unknown model with custom default",
			model:         "unknown",
			maxTokens:     1000,
			expectedLimit: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_ModelPrefixMatching(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "Claude 3.5 Sonnet variant",
			model:         "claude-3-5-sonnet-20240620",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
		{
			name:          "GPT-4o variant",
			model:         "gpt-4o-2024-05-13",
			maxTokens:     200000,
			expectedLimit: 128000,
		},
		{
			name:          "GPT-4 prefix",
			model:         "gpt-4-some-variant",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "GPT-3.5 prefix",
			model:         "gpt-3.5-turbo-instruct",
			maxTokens:     5000,
			expectedLimit: 4096,
		},
		{
			name:          "Gemini 1.5 prefix",
			model:         "gemini-1.5-pro-experimental",
			maxTokens:     10000,
			expectedLimit: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}

func TestMaxTokenInterceptor_NegativeMaxTokens(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	req := &anthropic.Request{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: -100,
	}

	err := interceptor.InterceptRequest(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Negative max_tokens should not be adjusted
	if req.MaxTokens != -100 {
		t.Errorf("expected max_tokens to remain -100, got %d", req.MaxTokens)
	}
}

func TestMaxTokenInterceptor_ProviderFallback(t *testing.T) {
	interceptor := NewMaxTokenInterceptor()

	tests := []struct {
		name          string
		model         string
		maxTokens     int
		expectedLimit int
	}{
		{
			name:          "GLM model (uses default provider limit)",
			model:         "glm-4-plus",
			maxTokens:     10000,
			expectedLimit: 8192, // GLM provider limit
		},
		{
			name:          "Qwen model (uses default provider limit)",
			model:         "qwen-plus",
			maxTokens:     10000,
			expectedLimit: 8192, // Qwen provider limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{
				Model:     tt.model,
				MaxTokens: tt.maxTokens,
			}

			err := interceptor.InterceptRequest(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if req.MaxTokens != tt.expectedLimit {
				t.Errorf("expected max_tokens %d, got %d", tt.expectedLimit, req.MaxTokens)
			}
		})
	}
}