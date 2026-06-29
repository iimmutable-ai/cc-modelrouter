package transformers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// GeminiTransformer transforms requests to Google Gemini API format.
type GeminiTransformer struct {
	*transformer.BaseTransformer
	messageStarted bool
}

// NewGeminiTransformer creates a new Gemini transformer.
func NewGeminiTransformer() *GeminiTransformer {
	return &GeminiTransformer{
		BaseTransformer: transformer.NewBaseTransformer("gemini"),
	}
}

// Gemini request/response types

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Tools             []geminiTool    `json:"tools,omitempty"`
	GenerationConfig  any             `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text              string                      `json:"text,omitempty"`
	InlineData        *geminiInlineData           `json:"inlineData,omitempty"`
	FunctionCall      *geminiFunctionCall         `json:"functionCall,omitempty"`
	FunctionResponses []*geminiFunctionResponse   `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type geminiResponse struct {
	Candidates     []geminiCandidate   `json:"candidates"`
	UsageMetadata  *geminiUsageMetadata `json:"usageMetadata,omitempty"`
	PromptFeedback *geminiPromptFeedback `json:"promptFeedback,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent `json:"content"`
	FinishReason  string        `json:"finishReason,omitempty"`
	SafetyRatings []any         `json:"safetyRatings,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiPromptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

// Endpoint returns the API endpoint path.
func (t *GeminiTransformer) Endpoint() string {
	return "/v1beta/models/%s:generateContent"
}

// PrepareRequest converts Anthropic request to Gemini HTTP request.
func (t *GeminiTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	// Convert Anthropic -> Gemini format
	geminiReq := t.convertToGemini(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Gemini uses models/{model}:generateContent endpoint with API key in query
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		strings.TrimSuffix(baseURL, "/"),
		url.PathEscape(model),
		url.QueryEscape(apiKey))

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// CRITICAL: Set GetBody to enable retries
	// The HTTP client may retry the request if there's a network error.
	// Without GetBody, the body reader would be exhausted on retry causing
	// "http: ContentLength=X with Body length 0" errors.
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", t.UserAgent())
	httpReq.Header.Set("Accept", "application/json")

	return httpReq, nil
}

// convertToGemini converts Anthropic request to Gemini format.
func (t *GeminiTransformer) convertToGemini(req *anthropic.Request) *geminiRequest {
	geminiReq := &geminiRequest{}

	// Convert system prompt
	if len(req.System) > 0 {
		geminiReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: string(req.System)}},
		}
	}

	// Convert messages
	for _, msg := range req.Messages {
		content := geminiContent{
			Role:  t.convertGeminiRole(string(msg.Role)),
			Parts: t.convertAnthropicToGeminiParts(msg.Content),
		}
		geminiReq.Contents = append(geminiReq.Contents, content)
	}

	// Convert tools
	if len(req.Tools) > 0 {
		tool := geminiTool{}
		for _, anthropicTool := range req.Tools {
			tool.FunctionDeclarations = append(tool.FunctionDeclarations, geminiFunctionDecl{
				Name:        anthropicTool.Name,
				Description: anthropicTool.Description,
				Parameters:  anthropicTool.InputSchema,
			})
		}
		geminiReq.Tools = []geminiTool{tool}
	}

	// Add generation config for max_tokens
	if req.MaxTokens > 0 {
		geminiReq.GenerationConfig = map[string]any{
			"maxOutputTokens": req.MaxTokens,
		}
	}

	return geminiReq
}

// convertGeminiRole converts Anthropic role to Gemini role.
func (t *GeminiTransformer) convertGeminiRole(role string) string {
	switch role {
	case "assistant":
		return "model"
	default:
		return role
	}
}

// convertAnthropicToGeminiParts converts Anthropic message content to Gemini parts.
func (t *GeminiTransformer) convertAnthropicToGeminiParts(content anthropic.MessageContent) []geminiPart {
	var parts []geminiPart

	for _, block := range content {
		switch block.Type {
		case "text":
			parts = append(parts, geminiPart{Text: block.Text})
		case "image":
			if block.Source != nil {
				parts = append(parts, geminiPart{
					InlineData: &geminiInlineData{
						MimeType: block.Source.MediaType,
						Data:     block.Source.Data,
					},
				})
			}
		}
	}

	return parts
}

// ParseResponse converts Gemini HTTP response to Anthropic response.
func (t *GeminiTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return t.convertGeminiToAnthropic(&geminiResp)
}

// convertGeminiToAnthropic converts Gemini response to Anthropic format.
func (t *GeminiTransformer) convertGeminiToAnthropic(resp *geminiResponse) (*anthropic.Response, error) {
	if len(resp.Candidates) == 0 {
		// Handle blocked content case
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return nil, fmt.Errorf("prompt blocked: %s", resp.PromptFeedback.BlockReason)
		}
		return &anthropic.Response{}, nil
	}

	candidate := resp.Candidates[0]
	result := &anthropic.Response{
		Type:       "message",
		Role:       anthropic.RoleAssistant,
		StopReason: t.mapGeminiFinishReason(candidate.FinishReason),
	}

	// Convert usage metadata
	if resp.UsageMetadata != nil {
		result.Usage = anthropic.Usage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		}
	}

	// Convert content parts
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			result.Content = append(result.Content, anthropic.ContentBlock{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.InlineData != nil {
			// Convert inline data to image block
			result.Content = append(result.Content, anthropic.ContentBlock{
				Type: "image",
				Source: &anthropic.ImageSource{
					Type:     "base64",
					MediaType: part.InlineData.MimeType,
					Data:      part.InlineData.Data,
				},
			})
		}
	}

	// Extract tool calls from function calls
	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			inputJSON, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				inputJSON = []byte("{}")
			}
			result.Content = append(result.Content, anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    fmt.Sprintf("toolu_%s", part.FunctionCall.Name),
				Name:  part.FunctionCall.Name,
				Input: json.RawMessage(inputJSON),
			})
		}
	}

	return result, nil
}

// mapGeminiFinishReason maps Gemini finish reasons to Anthropic format.
func (t *GeminiTransformer) mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "SAFETY":
		return "stop_sequence"
	case "RECITATION":
		return "stop_sequence"
	case "OTHER":
		return "end_turn"
	default:
		return reason
	}
}

// SupportsStreaming returns true.
func (t *GeminiTransformer) SupportsStreaming() bool {
	return true
}

// resetState clears the message started state.
func (t *GeminiTransformer) resetState() {
	t.messageStarted = false
}

// generateMessageStart creates a synthetic message_start event.
func (t *GeminiTransformer) generateMessageStart() (transformer.SSEEvent, error) {
	messageStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            "gemini-msg",
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         "gemini",
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	data, err := json.Marshal(messageStart)
	if err != nil {
		logging.StreamDebugf("[GEMINI TRANSFORM] Failed to marshal message_start: %v", err)
		return transformer.SSEEvent{}, fmt.Errorf("failed to marshal message_start event: %w", err)
	}
	if len(data) == 0 {
		logging.StreamDebugf("[GEMINI TRANSFORM] message_start marshaled to empty JSON")
		return transformer.SSEEvent{}, fmt.Errorf("message_start event marshaled to empty JSON")
	}
	return transformer.SSEEvent{
		EventType: "message_start",
		Data:      data,
	}, nil
}

// TransformStreamEvent transforms Gemini SSE events to Anthropic format.
func (t *GeminiTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	logging.StreamDebugf("[GEMINI TRANSFORM] Input: eventType='%s', data=%s", event.EventType, string(event.Data))

	// Parse the Gemini chunk
	var geminiChunk struct {
		Candidates    []geminiCandidate   `json:"candidates"`
		UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
	}
	if err := json.Unmarshal(event.Data, &geminiChunk); err != nil {
		// If parsing fails, might already be Anthropic format
		logging.StreamDebugf("[GEMINI TRANSFORM] Failed to parse as Gemini, passing through: %v", err)
		return []transformer.SSEEvent{*event}, nil
	}

	var result []transformer.SSEEvent

	// Extract text from candidates
	if len(geminiChunk.Candidates) > 0 {
		candidate := geminiChunk.Candidates[0]
		if len(candidate.Content.Parts) > 0 {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					// Convert to Anthropic content_block_delta format
					anthropicChunk := map[string]any{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]string{
							"type": "text_delta",
							"text": part.Text,
						},
					}
					data, err := json.Marshal(anthropicChunk)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
					}
					if len(data) == 0 {
						return nil, fmt.Errorf("content_block_delta event marshaled to empty JSON")
					}
					return []transformer.SSEEvent{
						{
							EventType: "content_block_delta",
							Data:      data,
						},
					}, nil
				}

				// Handle function calls in streaming
				if part.FunctionCall != nil {
					// Generate tool_use content block
					contentBlockStart := map[string]any{
						"type":  "content_block_start",
						"index": 0,
						"content_block": map[string]any{
							"type": "tool_use",
							"id":   fmt.Sprintf("toolu_%s", part.FunctionCall.Name),
							"name": part.FunctionCall.Name,
						},
					}
					startData, err := json.Marshal(contentBlockStart)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
					}
					if len(startData) == 0 {
						return nil, fmt.Errorf("content_block_start event marshaled to empty JSON")
					}

					// Marshal args
					argsJSON, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						argsJSON = []byte("{}")
					}

					contentBlockDelta := map[string]any{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": string(argsJSON),
						},
					}
					deltaData, err := json.Marshal(contentBlockDelta)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
					}
					if len(deltaData) == 0 {
						return nil, fmt.Errorf("content_block_delta event marshaled to empty JSON")
					}

					contentBlockStop := map[string]any{
						"type":  "content_block_stop",
						"index": 0,
					}
					stopData, err := json.Marshal(contentBlockStop)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
					}
					if len(stopData) == 0 {
						return nil, fmt.Errorf("content_block_stop event marshaled to empty JSON")
					}

					return []transformer.SSEEvent{
						{EventType: "content_block_start", Data: startData},
						{EventType: "content_block_delta", Data: deltaData},
						{EventType: "content_block_stop", Data: stopData},
					}, nil
				}
			}
		}

		// Check for finish reason (end of stream)
		if candidate.FinishReason != "" && candidate.FinishReason != "IN_PROGRESS" {
			stopData, err := json.Marshal(map[string]any{
				"type":  "content_block_stop",
				"index": 0,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
			}
			if len(stopData) == 0 {
				return nil, fmt.Errorf("content_block_stop event marshaled to empty JSON")
			}
			result = append(result, transformer.SSEEvent{
				EventType: "content_block_stop",
				Data:      stopData,
			})

			// Add usage if present
			if geminiChunk.UsageMetadata != nil {
				messageDelta := map[string]any{
					"type":  "message_delta",
					"delta": map[string]string{
						"stop_reason": t.mapGeminiFinishReason(candidate.FinishReason),
					},
					"usage": map[string]any{
						"output_tokens": geminiChunk.UsageMetadata.CandidatesTokenCount,
					},
				}
				deltaData, err := json.Marshal(messageDelta)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal message_delta event: %w", err)
				}
				if len(deltaData) == 0 {
					return nil, fmt.Errorf("message_delta event marshaled to empty JSON")
				}
				result = append(result, transformer.SSEEvent{
					EventType: "message_delta",
					Data:      deltaData,
				})
			}

			messageStopData, err := json.Marshal(map[string]string{
				"type": "message_stop",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to marshal message_stop event: %w", err)
			}
			if len(messageStopData) == 0 {
				return nil, fmt.Errorf("message_stop event marshaled to empty JSON")
			}
			result = append(result, transformer.SSEEvent{
				EventType: "message_stop",
				Data:      messageStopData,
			})
			return result, nil
		}
	}

	// No content to return
	if len(result) == 0 {
		logging.StreamDebugf("[GEMINI TRANSFORM] No transformation, passing through")
		return []transformer.SSEEvent{*event}, nil
	}

	logging.StreamDebugf("[GEMINI TRANSFORM] Transformed %d events", len(result))

	// Convert back to transformer.SSEEvent format
	var finalResult []transformer.SSEEvent
	for _, te := range result {
		// Add message_start for text content if not started
		if te.EventType == "content_block_delta" && !t.messageStarted {
			msgStart, err := t.generateMessageStart()
			if err != nil {
				return nil, fmt.Errorf("failed to generate message_start: %w", err)
			}
			if len(msgStart.Data) > 0 {
				finalResult = append(finalResult, msgStart)
				t.messageStarted = true
				logging.StreamDebugf("[GEMINI TRANSFORM] Generated message_start event")
			}
		}

		// Reset state on message_stop
		if te.EventType == "message_stop" {
			t.resetState()
		}

		finalResult = append(finalResult, te)
	}

	return finalResult, nil
}
