// Package interceptor implements utility interceptors for cross-cutting concerns.
package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// ReasoningConfig holds configuration for reasoning content extraction.
type ReasoningConfig struct {
	// Enabled enables reasoning extraction.
	Enabled bool
	// ExtractThinking extracts thinking content from responses.
	ExtractThinking bool
	// FormatForClaudeCode formats reasoning for Claude Code display.
	FormatForClaudeCode bool
}

// DefaultReasoningConfig returns a default configuration.
func DefaultReasoningConfig() *ReasoningConfig {
	return &ReasoningConfig{
		Enabled:               true,
		ExtractThinking:       true,
		FormatForClaudeCode:   true,
	}
}

// ReasoningInterceptor handles thinking/reasoning content extraction and formatting.
type ReasoningInterceptor struct {
	config *ReasoningConfig
}

// NewReasoningInterceptor creates a new ReasoningInterceptor with default configuration.
func NewReasoningInterceptor() *ReasoningInterceptor {
	return &ReasoningInterceptor{
		config: DefaultReasoningConfig(),
	}
}

// NewReasoningInterceptorWithConfig creates a new ReasoningInterceptor with custom configuration.
func NewReasoningInterceptorWithConfig(config *ReasoningConfig) *ReasoningInterceptor {
	return &ReasoningInterceptor{
		config: config,
	}
}

// InterceptResponse extracts and formats reasoning content from non-streaming responses.
func (i *ReasoningInterceptor) InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error {
	if !i.config.Enabled || !i.config.ExtractThinking {
		return nil
	}

	// Look for thinking content in the response
	// Some providers return thinking in special content blocks
	for idx := range resp.Content {
		if resp.Content[idx].Type == "thinking" && resp.Content[idx].Thinking != "" {
			logging.Debugf("[ReasoningInterceptor] Found thinking content in response, index=%d", idx)

			// If formatting for Claude Code, enhance the thinking content
			if i.config.FormatForClaudeCode {
				formatted := i.formatThinkingForClaudeCode(resp.Content[idx].Thinking)
				resp.Content[idx].Thinking = formatted
			}
		}
	}

	return nil
}

// InterceptStreamingEvent extracts and formats reasoning content from streaming events.
// This implements StreamingResponseInterceptor for real-time reasoning extraction.
func (i *ReasoningInterceptor) InterceptStreamingEvent(ctx context.Context, req *anthropic.Request, eventType string, data []byte) ([]byte, error) {
	if !i.config.Enabled || !i.config.ExtractThinking {
		return data, nil
	}

	// Parse the SSE event data
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return data, nil
	}

	// Look for reasoning-related content blocks
	if eventType == "content_block_delta" || event["type"] == "content_block_delta" {
		if delta, ok := event["delta"].(map[string]any); ok {
			if deltaType, ok := delta["type"].(string); ok && deltaType == "thinking_delta" {
				// Found thinking delta - extract and potentially format
				if thinkingText, ok := delta["thinking"].(string); ok && i.config.FormatForClaudeCode {
					delta["thinking"] = i.formatThinkingForClaudeCode(thinkingText)
					return json.Marshal(event)
				}
			}
		}
	}

	// Also check content_block_start for thinking blocks
	if eventType == "content_block_start" || event["type"] == "content_block_start" {
		if contentBlock, ok := event["content_block"].(map[string]any); ok {
			if blockType, ok := contentBlock["type"].(string); ok && blockType == "thinking" {
				logging.Debugf("[ReasoningInterceptor] Found thinking content block in stream")
				// No modification needed on start, just log for debugging
			}
		}
	}

	return data, nil
}

// formatThinkingForClaudeCode formats thinking content for display in Claude Code.
// It adds structure and makes the reasoning more readable.
func (i *ReasoningInterceptor) formatThinkingForClaudeCode(thinking string) string {
	if thinking == "" {
		return thinking
	}

	// If already formatted (contains thinking markers), return as-is
	if strings.Contains(thinking, "<thinking>") || strings.Contains(thinking, "---") {
		return thinking
	}

	// Format the thinking content with proper structure
	var builder strings.Builder

	// Add a header if content is substantial
	if len(thinking) > 100 {
		builder.WriteString("--- Thinking ---\n")
		builder.WriteString(thinking)
		builder.WriteString("\n--- End Thinking ---")
	} else {
		builder.WriteString(thinking)
	}

	result := builder.String()
	logging.Debugf("[ReasoningInterceptor] Formatted thinking content: %d chars", len(result))
	return result
}

// ExtractThinkingFromResponse extracts thinking content from a response.
// This is a utility method that can be called by other components.
func (i *ReasoningInterceptor) ExtractThinkingFromResponse(resp *anthropic.Response) string {
	var thinkingContent []string

	for _, block := range resp.Content {
		if block.Type == "thinking" && block.Thinking != "" {
			thinkingContent = append(thinkingContent, block.Thinking)
		}
	}

	if len(thinkingContent) == 0 {
		return ""
	}

	return strings.Join(thinkingContent, "\n")
}

// ExtractReasoningFromDelta extracts reasoning from a content_block_delta event.
func (i *ReasoningInterceptor) ExtractReasoningFromDelta(delta map[string]any) string {
	if deltaType, ok := delta["type"].(string); ok && deltaType == "thinking_delta" {
		if thinking, ok := delta["thinking"].(string); ok {
			return thinking
		}
	}

	// Also check for partial_json which may contain thinking
	if partialJSON, ok := delta["partial_json"].(string); ok {
		// Try to parse as JSON to see if it contains thinking
		var parsed map[string]any
		if err := json.Unmarshal([]byte(partialJSON), &parsed); err == nil {
			if thinking, ok := parsed["thinking"].(string); ok {
				return thinking
			}
		}
	}

	return ""
}

// ParseReasoningEvent parses a reasoning SSE event and returns the extracted content.
func (i *ReasoningInterceptor) ParseReasoningEvent(eventType string, data []byte) (string, error) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return "", fmt.Errorf("failed to parse event: %w", err)
	}

	// Handle content_block_delta with thinking
	if eventType == "content_block_delta" || event["type"] == "content_block_delta" {
		if delta, ok := event["delta"].(map[string]any); ok {
			if thinking := i.ExtractReasoningFromDelta(delta); thinking != "" {
				return thinking, nil
			}
		}
	}

	// Handle content_block events
	if eventType == "content_block" || event["type"] == "content_block" {
		if content, ok := event["content"].(string); ok {
			return content, nil
		}
	}

	return "", nil
}