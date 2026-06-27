// Package interceptor implements utility interceptors for cross-cutting concerns.
package interceptor

import (
	"context"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// MaxTokenConfig holds configuration for max token limits by provider/model.
type MaxTokenConfig struct {
	// DefaultLimit is the fallback max_tokens limit when no specific limit is found.
	DefaultLimit int
	// ProviderLimits maps provider names to their max token limits.
	ProviderLimits map[string]int
	// ModelLimits maps specific model names to their max token limits.
	// These override provider limits when matched.
	ModelLimits map[string]int
}

// DefaultMaxTokenConfig returns a default configuration with common provider limits.
func DefaultMaxTokenConfig() *MaxTokenConfig {
	return &MaxTokenConfig{
		DefaultLimit: 4096,
		ProviderLimits: map[string]int{
			"anthropic":  8192,
			"openai":     4096,
			"openrouter": 4096,
			"gemini":     8192,
			"glm":        8192,
			"qwen":       8192,
			"minimax":    8192,
		},
		ModelLimits: map[string]int{
			// Claude models with specific limits
			"claude-3-5-sonnet-20241022": 8192,
			"claude-3-5-sonnet-20240620":  8192,
			"claude-3-5-haiku-20241022":   8192,
			"claude-3-5-haiku-20240307":    8192,
			"claude-3-opus-20240229":      4096,
			"claude-3-sonnet-20240229":    4096,
			"claude-3-haiku-20240307":     4096,

			// OpenAI models with specific limits
			"gpt-4-turbo-preview":  4096,
			"gpt-4-0125-preview":    4096,
			"gpt-4-1106-preview":    4096,
			"gpt-4-vision-preview":  4096,
			"gpt-4-0314":            4096,
			"gpt-4-0613":            4096,
			"gpt-3.5-turbo-0125":    4096,
			"gpt-3.5-turbo-1106":    4096,
			"gpt-3.5-turbo-16k":     16384,
			"gpt-4-32k":             32768,
			"gpt-4-turbo":           128000,
			"gpt-4o":                128000,
			"gpt-4o-mini":           16000,

			// Gemini models
			"gemini-1.5-pro":        8192,
			"gemini-1.5-flash":      8192,
			"gemini-1.0-pro":        2048,
			"gemini-pro":            2048,
		},
	}
}

// MaxTokenInterceptor adjusts max_tokens based on provider/model limits.
type MaxTokenInterceptor struct {
	config *MaxTokenConfig
}

// NewMaxTokenInterceptor creates a new MaxTokenInterceptor with default configuration.
func NewMaxTokenInterceptor() *MaxTokenInterceptor {
	return &MaxTokenInterceptor{
		config: DefaultMaxTokenConfig(),
	}
}

// NewMaxTokenInterceptorWithConfig creates a new MaxTokenInterceptor with custom configuration.
func NewMaxTokenInterceptorWithConfig(config *MaxTokenConfig) *MaxTokenInterceptor {
	return &MaxTokenInterceptor{
		config: config,
	}
}

// InterceptRequest adjusts max_tokens based on provider/model limits.
func (i *MaxTokenInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	if req.MaxTokens <= 0 {
		return nil // No max_tokens specified, nothing to do
	}

	limit := i.getMaxTokenLimit(req.Model)
	if req.MaxTokens > limit {
		oldTokens := req.MaxTokens
		req.MaxTokens = limit
		logging.Infof("[MaxTokenInterceptor] Adjusted max_tokens from %d to %d for model %s",
			oldTokens, limit, req.Model)
	}

	return nil
}

// getMaxTokenLimit returns the max token limit for the given model.
// It first checks exact model match, then provider limits, then default.
func (i *MaxTokenInterceptor) getMaxTokenLimit(model string) int {
	// Check exact model match first
	if limit, ok := i.config.ModelLimits[model]; ok {
		return limit
	}

	// Check if model starts with a known provider prefix
	lowerModel := strings.ToLower(model)

	// Check for specific model prefixes
	if strings.HasPrefix(lowerModel, "claude-3-5-sonnet") || strings.HasPrefix(lowerModel, "claude-3-5-haiku") {
		if limit, ok := i.config.ProviderLimits["anthropic"]; ok {
			return limit
		}
	}
	if strings.HasPrefix(lowerModel, "claude-3-") {
		// Claude 3 non-5 models have lower limits
		return 4096
	}
	if strings.HasPrefix(lowerModel, "gpt-4o") {
		return 128000
	}
	if strings.HasPrefix(lowerModel, "gpt-3.5") {
		if strings.Contains(lowerModel, "16k") {
			return 16384
		}
		return 4096
	}
	if strings.HasPrefix(lowerModel, "gpt-4") {
		if strings.Contains(lowerModel, "32k") {
			return 32768
		}
		return 4096
	}
	if strings.HasPrefix(lowerModel, "gemini-1.5") {
		return 8192
	}
	if strings.HasPrefix(lowerModel, "gemini-1.0") || strings.HasPrefix(lowerModel, "gemini-pro") {
		return 2048
	}
	if strings.HasPrefix(lowerModel, "glm-") {
		// GLM models use their provider limit
		if limit, ok := i.config.ProviderLimits["glm"]; ok {
			return limit
		}
	}
	if strings.HasPrefix(lowerModel, "qwen-") {
		// Qwen models use their provider limit
		if limit, ok := i.config.ProviderLimits["qwen"]; ok {
			return limit
		}
	}

	// Fall back to default
	return i.config.DefaultLimit
}