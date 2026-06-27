// Package util provides testing utilities for integration tests.
package util

import (
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// TestConfigBuilder helps build test configurations.
type TestConfigBuilder struct {
	cfg *config.Config
}

// NewTestConfigBuilder creates a new test configuration builder.
func NewTestConfigBuilder() *TestConfigBuilder {
	return &TestConfigBuilder{
		cfg: config.Defaults(),
	}
}

// WithServer sets the server configuration.
func (b *TestConfigBuilder) WithServer(host string, port int) *TestConfigBuilder {
	b.cfg.Server.Host = host
	b.cfg.Server.Port = port
	return b
}

// WithProvider adds a provider configuration.
func (b *TestConfigBuilder) WithProvider(name, baseURL, apiKey string, models []string, transformer string) *TestConfigBuilder {
	if b.cfg.Providers == nil {
		b.cfg.Providers = make(map[string]config.ProviderConfig)
	}
	b.cfg.Providers[name] = config.ProviderConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Models:      models,
		Transformer: transformer,
	}
	return b
}

// WithRoute adds a route configuration.
func (b *TestConfigBuilder) WithRoute(model, target string) *TestConfigBuilder {
	if b.cfg.Router.Routes == nil {
		b.cfg.Router.Routes = make(map[string]string)
	}
	b.cfg.Router.Routes[model] = target
	return b
}

// WithRetryConfig sets the retry configuration.
func (b *TestConfigBuilder) WithRetryConfig(maxRetries int, retryDelay string) *TestConfigBuilder {
	b.cfg.Router.MaxRetries = maxRetries
	b.cfg.Router.RetryDelay = retryDelay
	return b
}

// WithLogging sets the logging configuration.
func (b *TestConfigBuilder) WithLogging(enabled bool, destination, level string) *TestConfigBuilder {
	b.cfg.Logging = config.LoggingConfig{
		Enabled:     enabled,
		Destination: destination,
		Level:       level,
	}
	return b
}

// Build returns the built configuration.
func (b *TestConfigBuilder) Build() *config.Config {
	return b.cfg
}

// MinimalTestConfig returns a minimal test configuration for testing.
func MinimalTestConfig() *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("test-provider", "http://localhost:9999", "test-key", []string{"test-model"}, "anthropic").
		WithRoute("test-model", "test-provider:test-model").
		Build()
}

// OpenRouterTestConfig returns a configuration for OpenRouter testing.
// Note: This uses the Anthropic-compatible endpoint which only supports Anthropic models.
// For non-Anthropic models (Google, OpenAI, etc.), use the OpenAI-compatible endpoint
// with a separate provider configuration.
func OpenRouterTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openrouter", "https://openrouter.ai/api", apiKey, []string{
			"anthropic/claude-3.5-sonnet",
			"anthropic/claude-opus-4",
			"anthropic/claude-haiku-4.5",
		}, "anthropic").
		WithRoute("claude-3.5-sonnet", "openrouter:anthropic/claude-3.5-sonnet").
		WithRoute("claude-opus-4", "openrouter:anthropic/claude-opus-4").
		Build()
}

// BigModelTestConfig returns a configuration for BigModel (GLM) testing.
func BigModelTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("bigmodel", "https://open.bigmodel.cn/api/anthropic", apiKey, []string{
			"glm-4",
			"glm-4-air",
			"glm-4-flash",
		}, "anthropic").
		WithRoute("glm-4", "bigmodel:glm-4").
		WithRoute("glm-4-air", "bigmodel:glm-4-air").
		Build()
}

// AliyunTestConfig returns a configuration for Aliyun (Qwen) testing.
func AliyunTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", apiKey, []string{
			"qwen-turbo",
			"qwen-plus",
			"qwen-max",
		}, "anthropic").
		WithRoute("qwen-plus", "aliyun:qwen-plus").
		WithRoute("qwen-max", "aliyun:qwen-max").
		Build()
}

// AnthropicTestConfig returns a configuration for Anthropic testing.
func AnthropicTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("anthropic", "https://api.anthropic.com", apiKey, []string{
			"claude-3-5-sonnet-20241022",
			"claude-3-5-haiku-20241022",
		}, "anthropic").
		WithRoute("claude-3-5-sonnet-20241022", "anthropic:claude-3-5-sonnet-20241022").
		Build()
}

// OpenAITestConfig returns a configuration for OpenAI testing.
func OpenAITestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("openai", "https://api.openai.com/v1", apiKey, []string{
			"gpt-4o",
			"gpt-4o-mini",
		}, "openai").
		WithRoute("gpt-4o", "openai:gpt-4o").
		Build()
}

// GeminiTestConfig returns a configuration for Gemini testing.
func GeminiTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("gemini", "https://generativelanguage.googleapis.com/v1beta", apiKey, []string{
			"gemini-2.0-flash-exp",
			"gemini-1.5-pro",
		}, "gemini").
		WithRoute("gemini-2.0-flash-exp", "gemini:gemini-2.0-flash-exp").
		Build()
}

// MiniMaxTestConfig returns a configuration for MiniMax testing.
func MiniMaxTestConfig(apiKey string) *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("minimax", "https://api.minimax.chat/v1", apiKey, []string{
			"abab6.5s-chat",
		}, "minimax").
		WithRoute("abab6.5s-chat", "minimax:abab6.5s-chat").
		Build()
}

// FailoverTestConfig returns a configuration for failover testing.
func FailoverTestConfig() *config.Config {
	return NewTestConfigBuilder().
		WithServer("localhost", 18081).
		WithProvider("primary", "http://localhost:9999", "key1", []string{"model"}, "anthropic").
		WithProvider("backup", "http://localhost:9998", "key2", []string{"model"}, "anthropic").
		WithRoute("model", "primary:model;backup:model").
		WithRetryConfig(3, "100ms").
		Build()
}