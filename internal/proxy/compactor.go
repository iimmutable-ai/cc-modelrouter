package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// Compactor reduces request size to fit within provider limits.
type Compactor interface {
	// Compact reduces the request size to fit within the provider's limit.
	// Returns the compacted request and true if compaction occurred, or an error.
	Compact(req *anthropic.Request) (*anthropic.Request, bool, error)

	// ShouldCompact returns true if the request exceeds the provider's limit.
	ShouldCompact(reqSize int64) bool
}

// compactor implements the Compactor interface with configurable strategy.
type compactor struct {
	providerName string
	providerCfg  config.ProviderConfig
	handler      *Handler
	strategy     compactStrategy
}

// compactStrategy defines the interface for compaction algorithms.
type compactStrategy interface {
	// Name returns the strategy name.
	Name() string

	// Compact performs the actual compaction.
	Compact(req *anthropic.Request, handler *Handler, providerName string, providerCfg config.ProviderConfig) (*anthropic.Request, bool, error)
}

// NewCompactor creates a new compactor for the given provider.
// Returns nil if the provider has no compaction configured.
func NewCompactor(handler *Handler, providerName string, providerCfg config.ProviderConfig) Compactor {
	if providerCfg.MaxRequestBodyBytes <= 0 {
		return nil
	}

	var strategy compactStrategy
	if providerCfg.Compaction != nil && providerCfg.Compaction.Method == "trim" {
		strategy = &trimStrategy{}
	} else {
		// Default to LLM-based compaction
		strategy = &llmStrategy{}
	}

	return &compactor{
		providerName: providerName,
		providerCfg:  providerCfg,
		handler:      handler,
		strategy:     strategy,
	}
}

// ShouldCompact returns true if the request size exceeds the limit.
func (c *compactor) ShouldCompact(reqSize int64) bool {
	return c.providerCfg.MaxRequestBodyBytes > 0 && reqSize > c.providerCfg.MaxRequestBodyBytes
}

// Compact reduces the request size using the configured strategy.
func (c *compactor) Compact(req *anthropic.Request) (*anthropic.Request, bool, error) {
	return c.strategy.Compact(req, c.handler, c.providerName, c.providerCfg)
}

// llmStrategy uses an LLM to summarize conversation history.
type llmStrategy struct{}

func (s *llmStrategy) Name() string {
	return "llm"
}

func (s *llmStrategy) Compact(req *anthropic.Request, handler *Handler, providerName string, providerCfg config.ProviderConfig) (*anthropic.Request, bool, error) {
	logging.Debugf("[COMPACTOR][LLM] Starting compaction for provider %s", providerName)

	// Create a deep copy to avoid modifying the original
	compacted, err := deepCopyRequest(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to copy request for compaction: %w", err)
	}

	// Determine which provider/model to use for summarization
	summarizeProvider := providerName
	summarizeModel := ""

	if providerCfg.Compaction != nil {
		if providerCfg.Compaction.SummarizeProvider != "" {
			summarizeProvider = providerCfg.Compaction.SummarizeProvider
		}
		if providerCfg.Compaction.SummarizeModel != "" {
			summarizeModel = providerCfg.Compaction.SummarizeModel
		}
	}

	// Find the provider config for summarization
	summarizeProviderCfg, ok := handler.config.Providers[summarizeProvider]
	if !ok {
		// Fall back to the original provider if summarize provider not found
		logging.Warnf("[COMPACTOR][LLM] Summarize provider %s not found, falling back to %s", summarizeProvider, providerName)
		summarizeProvider = providerName
		summarizeProviderCfg = providerCfg
	}

	// Use first available model if none specified
	if summarizeModel == "" && len(summarizeProviderCfg.Models) > 0 {
		summarizeModel = summarizeProviderCfg.Models[0]
	}

	// Build the summary prompt
	summaryPrompt := buildSummaryPrompt(compacted.Messages)

	// Create a summary request
	summaryReq := &anthropic.Request{
		Model:     summarizeModel,
		MaxTokens: 1000,
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: summaryPrompt},
				},
			},
		},
	}

	// Get transformer for the summarization provider
	transformerName := summarizeProviderCfg.Transformer
	if transformerName == "" {
		transformerName = summarizeProvider
	}
	tf, err := handler.transformerRegistry.Get(transformerName)
	if err != nil {
		// Fall back to trim strategy if LLM compaction fails
		logging.Warnf("[COMPACTOR][LLM] Failed to get transformer for %s, falling back to trim: %v", summarizeProvider, err)
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}

	// Prepare the summary request
	httpReq, err := tf.PrepareRequest(summaryReq, summarizeProviderCfg.BaseURL, summarizeProviderCfg.APIKey, summarizeModel)
	if err != nil {
		logging.Warnf("[COMPACTOR][LLM] Failed to prepare summary request: %v", err)
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}

	// Execute the summary request
	client := handler.providerClients[summarizeProvider]
	if client == nil {
		logging.Warnf("[COMPACTOR][LLM] No client for provider %s, falling back to trim", summarizeProvider)
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}

	ctx := context.Background()
	resp, err := client.DoWithContext(ctx, httpReq)
	if err != nil {
		logging.Warnf("[COMPACTOR][LLM] Summary request failed: %v", err)
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}
	defer resp.Body.Close()

	// Parse the response
	anthropicResp, err := tf.ParseResponse(resp)
	if err != nil {
		logging.Warnf("[COMPACTOR][LLM] Failed to parse summary response: %v", err)
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}

	// Extract the summary text
	summary := extractTextFromResponse(anthropicResp)
	if summary == "" {
		logging.Warnf("[COMPACTOR][LLM] Empty summary response, falling back to trim")
		trim := &trimStrategy{}
		return trim.Compact(req, handler, providerName, providerCfg)
	}

	// Create a system message with the summary
	summaryMessage := anthropic.Message{
		Role: anthropic.RoleUser,
		Content: []anthropic.ContentBlock{
			{
				Type: "text",
				Text: fmt.Sprintf("[Previous conversation summarized]: %s", summary),
			},
		},
	}

	// Keep the last user message and assistant response (if any)
	// to maintain conversation continuity
	var keptMessages []anthropic.Message
	keptMessages = append(keptMessages, summaryMessage)

	// Keep up to 2 most recent messages for context
	msgCount := len(compacted.Messages)
	if msgCount > 0 {
		startIdx := msgCount - 2
		if startIdx < 0 {
			startIdx = 0
		}
		keptMessages = append(keptMessages, compacted.Messages[startIdx:]...)
	}

	compacted.Messages = keptMessages

	logging.Infof("[COMPACTOR][LLM] Compacted request from %d to %d messages", len(req.Messages), len(compacted.Messages))

	return compacted, true, nil
}

// trimStrategy removes oldest messages to reduce request size.
type trimStrategy struct{}

func (s *trimStrategy) Name() string {
	return "trim"
}

func (s *trimStrategy) Compact(req *anthropic.Request, handler *Handler, providerName string, providerCfg config.ProviderConfig) (*anthropic.Request, bool, error) {
	logging.Debugf("[COMPACTOR][TRIM] Starting trim compaction for provider %s", providerName)

	// Create a deep copy to avoid modifying the original
	compacted, err := deepCopyRequest(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to copy request for compaction: %w", err)
	}

	if len(compacted.Messages) <= 2 {
		// Can't trim further, keep only the most recent message
		if len(compacted.Messages) > 0 {
			compacted.Messages = compacted.Messages[len(compacted.Messages)-1:]
		}
		logging.Infof("[COMPACTOR][TRIM] Trimmed to %d messages (minimum reached)", len(compacted.Messages))
		return compacted, true, nil
	}

	// Keep system message if present (as first message from user with "system" role context)
	// and keep the last 2 messages for context
	msgCount := len(compacted.Messages)
	keepCount := 2

	// Determine how many messages to remove
	removeCount := msgCount - keepCount
	if removeCount < 1 {
		removeCount = 1
	}

	// Create summary message about what was removed
	summaryText := fmt.Sprintf("[Previous %d messages removed due to size constraints]", removeCount)
	summaryMessage := anthropic.Message{
		Role: anthropic.RoleUser,
		Content: []anthropic.ContentBlock{
			{Type: "text", Text: summaryText},
		},
	}

	// Keep the last 'keepCount' messages
	startIdx := msgCount - keepCount
	if startIdx < 0 {
		startIdx = 0
	}

	var newMessages []anthropic.Message
	newMessages = append(newMessages, summaryMessage)
	newMessages = append(newMessages, compacted.Messages[startIdx:]...)
	compacted.Messages = newMessages

	logging.Infof("[COMPACTOR][TRIM] Trimmed request from %d to %d messages", len(req.Messages), len(compacted.Messages))

	return compacted, true, nil
}

// buildSummaryPrompt creates a prompt for summarizing conversation history.
func buildSummaryPrompt(messages []anthropic.Message) string {
	var sb strings.Builder

	sb.WriteString("Please provide a concise summary of the following conversation. " +
		"Focus on key points, decisions made, and important context that would be needed " +
		"to continue the conversation. Keep the summary under 500 words.\n\n")
	sb.WriteString("Conversation:\n\n")

	for _, msg := range messages {
		role := msg.Role
		text := extractTextFromMessage(msg)
		if text != "" {
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", role, truncateString(text, 2000)))
		}
	}

	return sb.String()
}

// extractTextFromMessage extracts text content from a message.
func extractTextFromMessage(msg anthropic.Message) string {
	var texts []string
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, " ")
}

// extractTextFromResponse extracts text from an Anthropic response.
func extractTextFromResponse(resp *anthropic.Response) string {
	var texts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, " ")
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// estimateRequestSize estimates the size of a request in bytes.
func estimateRequestSize(req *anthropic.Request) int64 {
	data, _ := json.Marshal(req)
	return int64(len(data))
}

// CompactRequestIfNeeded checks if compaction is needed and performs it.
// This is a helper function used by the handler during failover.
func CompactRequestIfNeeded(req *anthropic.Request, handler *Handler, providerName string, providerCfg config.ProviderConfig) (*anthropic.Request, bool, error) {
	compactor := NewCompactor(handler, providerName, providerCfg)
	if compactor == nil {
		return req, false, nil
	}

	size := estimateRequestSize(req)
	if !compactor.ShouldCompact(size) {
		return req, false, nil
	}

	logging.Debugf("[COMPACTOR] Request size %d bytes exceeds limit %d bytes for provider %s, compacting...",
		size, providerCfg.MaxRequestBodyBytes, providerName)

	return compactor.Compact(req)
}
