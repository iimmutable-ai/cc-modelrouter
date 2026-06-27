package transformers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// OpenAITransformer transforms requests to OpenAI API format.
type OpenAITransformer struct {
	*transformer.BaseTransformer
	toolCallStates map[int]*openAIToolCallState
	messageStarted bool
}

// openAIToolCallState tracks the state of a streaming tool call.
type openAIToolCallState struct {
	id        string
	name      string
	arguments strings.Builder
	started   bool
	hasID     bool
	hasName   bool
}

// NewOpenAITransformer creates a new OpenAI transformer.
func NewOpenAITransformer() *OpenAITransformer {
	return &OpenAITransformer{
		BaseTransformer: transformer.NewBaseTransformer("openai"),
		toolCallStates:  make(map[int]*openAIToolCallState),
	}
}

// OpenAI request/response types

type openAIRequest struct {
	Model      string        `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
	Stream     bool          `json:"stream,omitempty"`
	Tools      []openAITool  `json:"tools,omitempty"`
	ToolChoice any           `json:"tool_choice,omitempty"`
}

type openAIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []interface{}
}

// openAIContentItem represents a content item in OpenAI's array format.
type openAIContentItem struct {
	Type       string            `json:"type"`
	Text       string            `json:"text,omitempty"`
	ImageURL   *openAIImageURL   `json:"image_url,omitempty"`
}

// openAIImageURL represents an image URL in OpenAI format.
type openAIImageURL struct {
	URL string `json:"url"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string             `json:"role"`
			Content   string             `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Endpoint returns the API endpoint path.
func (t *OpenAITransformer) Endpoint() string {
	return "/v1/chat/completions"
}

// PrepareRequest converts Anthropic request to OpenAI HTTP request.
func (t *OpenAITransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	// Convert Anthropic -> OpenAI format
	openaiReq := t.convertToOpenAI(req, model)

	body, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := baseURL
	if !strings.HasSuffix(baseURL, "/chat/completions") {
		endpoint = strings.TrimSuffix(baseURL, "/") + "/chat/completions"
	}

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
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("User-Agent", "cc-modelrouter/1.0")
	httpReq.Header.Set("Accept", "application/json")

	return httpReq, nil
}

// convertToOpenAI converts Anthropic request to OpenAI format.
func (t *OpenAITransformer) convertToOpenAI(req *anthropic.Request, model string) *openAIRequest {
	openaiReq := &openAIRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	// Convert messages
	for _, msg := range req.Messages {
		content := t.convertContent(msg.Content)
		openaiReq.Messages = append(openaiReq.Messages, openAIMessage{
			Role:    string(msg.Role),
			Content: content,
		})
	}

	// Convert tools
	for _, tool := range req.Tools {
		openaiTool := openAITool{Type: "function"}
		openaiTool.Function.Name = tool.Name
		openaiTool.Function.Description = tool.Description
		openaiTool.Function.Parameters = tool.InputSchema
		openaiReq.Tools = append(openaiReq.Tools, openaiTool)
	}

	return openaiReq
}

// convertContent converts Anthropic message content to OpenAI format.
// Returns either a string (for text-only content) or an array of content items (for multimodal).
func (t *OpenAITransformer) convertContent(content anthropic.MessageContent) interface{} {
	// Check if content is text-only
	hasOnlyText := true
	for _, block := range content {
		if block.Type != "text" {
			hasOnlyText = false
			break
		}
	}

	// Fast path: text-only content can be a simple string
	if hasOnlyText {
		var texts []string
		for _, block := range content {
			texts = append(texts, block.Text)
		}
		return strings.Join(texts, "\n")
	}

	// Multimodal content: convert to array of content items
	var items []openAIContentItem
	for _, block := range content {
		switch block.Type {
		case "text":
			items = append(items, openAIContentItem{
				Type: "text",
				Text: block.Text,
			})

		case "image":
			// Convert Anthropic image format to OpenAI image_url format
			// Anthropic: {"type": "base64", "media_type": "image/jpeg", "data": "..."}
			// OpenAI: {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}}
			if block.Source != nil {
				dataURL := fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
				items = append(items, openAIContentItem{
					Type: "image_url",
					ImageURL: &openAIImageURL{
						URL: dataURL,
					},
				})
			}

		case "document":
			// Document blocks with file_id are part of Anthropic's Files API.
			// NOTE: Claude Code does NOT use Files API, so this case only occurs
			// when using cc-modelrouter directly (not via Claude Code).
			// File resolution is not implemented - we use placeholder text for graceful degradation.
			placeholderText := ""
			if block.Title != "" {
				placeholderText = fmt.Sprintf("[Document: %s - file_id: %s]", block.Title, block.DocumentSource.FileID)
			} else {
				placeholderText = fmt.Sprintf("[Document with file_id: %s]", block.DocumentSource.FileID)
			}
			items = append(items, openAIContentItem{
				Type: "text",
				Text: placeholderText,
			})
			logging.Streamf("[OPENAI TRANSFORM] Document block with file_id '%s' detected. Files API is not supported for non-Anthropic providers. Using placeholder text.", block.DocumentSource.FileID)

		case "thinking":
			// Thinking blocks are Anthropic-specific and should not be sent to other providers
			logging.StreamDebugf("[OPENAI TRANSFORM] Thinking block omitted from provider request")

		case "tool_use":
			// Tool use blocks are handled separately via the Tools array
			logging.StreamDebugf("[OPENAI TRANSFORM] Tool use block handled via Tools array")

		case "tool_result":
			// Tool results should be inlined as text
			if block.Content != nil && len(block.Content) > 0 {
				resultText := t.extractTextFromContent(block.Content)
				items = append(items, openAIContentItem{
					Type: "text",
					Text: resultText,
				})
			}

		default:
			// Unknown block type - log and skip
			logging.StreamDebugf("[OPENAI TRANSFORM] Unknown content block type '%s', skipping", block.Type)
		}
	}

	return items
}

// extractTextFromContent extracts text from MessageContent, used for tool results.
func (t *OpenAITransformer) extractTextFromContent(content anthropic.MessageContent) string {
	var texts []string
	for _, block := range content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ParseResponse converts OpenAI HTTP response to Anthropic response.
func (t *OpenAITransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var openaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return t.convertOpenAItoAnthropic(&openaiResp)
}

// convertOpenAItoAnthropic converts OpenAI response to Anthropic format.
func (t *OpenAITransformer) convertOpenAItoAnthropic(resp *openAIResponse) (*anthropic.Response, error) {
	if len(resp.Choices) == 0 {
		return &anthropic.Response{}, nil
	}

	choice := resp.Choices[0]
	result := &anthropic.Response{
		ID:         resp.ID,
		Type:       "message",
		Role:       anthropic.RoleAssistant,
		Model:      resp.Model,
		StopReason: t.mapStopReason(choice.FinishReason),
		Usage: anthropic.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}

	// Handle regular text content
	if choice.Message.Content != "" {
		result.Content = append(result.Content, anthropic.ContentBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}

	// Handle tool calls
	for _, toolCall := range choice.Message.ToolCalls {
		arguments := toolCall.Function.Arguments
		if arguments == "" {
			arguments = "{}"
		} else if !json.Valid([]byte(arguments)) {
			arguments = "{}"
		}
		result.Content = append(result.Content, anthropic.ContentBlock{
			Type:  "tool_use",
			ID:    toolCall.ID,
			Name:  toolCall.Function.Name,
			Input: json.RawMessage(arguments),
		})
	}

	return result, nil
}

// SupportsStreaming returns true.
func (t *OpenAITransformer) SupportsStreaming() bool {
	return true
}

// resetToolCallStates clears the tool call state map.
func (t *OpenAITransformer) resetToolCallStates() {
	t.toolCallStates = make(map[int]*openAIToolCallState)
	t.messageStarted = false
}

// generateMessageStart creates a synthetic message_start event.
func (t *OpenAITransformer) generateMessageStart() (transformer.SSEEvent, error) {
	messageStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            "openai-msg",
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         "openai",
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
		logging.StreamDebugf("[OPENAI TRANSFORM] Failed to marshal message_start: %v", err)
		return transformer.SSEEvent{}, fmt.Errorf("failed to marshal message_start event: %w", err)
	}
	if len(data) == 0 {
		logging.StreamDebugf("[OPENAI TRANSFORM] message_start marshaled to empty JSON")
		return transformer.SSEEvent{}, fmt.Errorf("message_start event marshaled to empty JSON")
	}
	return transformer.SSEEvent{
		EventType: "message_start",
		Data:      data,
	}, nil
}

// transformToolCallChunks transforms OpenAI tool_call chunks to Anthropic format.
func (t *OpenAITransformer) transformToolCallChunks(toolCalls []any) ([]transformer.SSEEvent, error) {
	var result []transformer.SSEEvent

	for _, tc := range toolCalls {
		toolCall, ok := tc.(map[string]any)
		if !ok {
			logging.StreamDebugf("[OPENAI TRANSFORM] Tool call is not a map, skipping: %T", tc)
			continue
		}

		index, hasIndex := toolCall["index"].(float64)
		if !hasIndex {
			logging.StreamDebugf("[OPENAI TRANSFORM] Tool call missing index, skipping")
			continue
		}
		toolIndex := int(index)

		state, exists := t.toolCallStates[toolIndex]
		if !exists {
			state = &openAIToolCallState{
				arguments: strings.Builder{},
			}
			t.toolCallStates[toolIndex] = state
		}

		toolID, hasID := toolCall["id"].(string)
		_, _ = toolCall["type"].(string)

		var functionName string
		var functionArgs string
		if function, hasFunction := toolCall["function"].(map[string]any); hasFunction {
			if name, ok := function["name"].(string); ok {
				functionName = name
			}
			if args, ok := function["arguments"].(string); ok {
				functionArgs = args
			}
		}

		logging.StreamDebugf("[OPENAI TRANSFORM] Tool call chunk: index=%d, id=%s, name=%s, args='%s'",
			toolIndex, toolID, functionName, functionArgs)

		if hasID && toolID != "" {
			state.id = toolID
			state.hasID = true
			logging.StreamDebugf("[OPENAI TRANSFORM] Updated state with id: %s", toolID)
		}
		if functionName != "" {
			state.name = functionName
			state.hasName = true
			logging.StreamDebugf("[OPENAI TRANSFORM] Updated state with name: %s", functionName)
		}

		if !state.started && state.hasID && state.hasName {
			state.started = true

			contentBlockStart := map[string]any{
				"type":  "content_block_start",
				"index": toolIndex,
				"content_block": map[string]any{
					"type": "tool_use",
					"id":   state.id,
					"name": state.name,
				},
			}
			startData, err := json.Marshal(contentBlockStart)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
			}
			if len(startData) == 0 {
				return nil, fmt.Errorf("content_block_start event marshaled to empty JSON")
			}
			result = append(result, transformer.SSEEvent{
				EventType: "content_block_start",
				Data:      startData,
			})
			logging.StreamDebugf("[OPENAI TRANSFORM] Generated content_block_start for tool_use: %s (id: %s)", state.name, state.id)
		}

		if functionArgs != "" {
			state.arguments.WriteString(functionArgs)

			contentBlockDelta := map[string]any{
				"type":  "content_block_delta",
				"index": toolIndex,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": functionArgs,
				},
			}
			deltaData, err := json.Marshal(contentBlockDelta)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			if len(deltaData) == 0 {
				return nil, fmt.Errorf("content_block_delta event marshaled to empty JSON")
			}
			result = append(result, transformer.SSEEvent{
				EventType: "content_block_delta",
				Data:      deltaData,
			})
			logging.StreamDebugf("[OPENAI TRANSFORM] Generated content_block_delta with partial_json: '%s'", functionArgs)
		}
	}

	if len(result) == 0 {
		logging.StreamDebugf("[OPENAI TRANSFORM] No events generated from tool call chunks")
		return []transformer.SSEEvent{}, nil
	}

	return result, nil
}

// mapStopReason maps OpenAI finish reasons to Anthropic format.
func (t *OpenAITransformer) mapStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// TransformStreamEvent transforms OpenAI SSE events to Anthropic format.
func (t *OpenAITransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	logging.StreamDebugf("[OPENAI TRANSFORM] Input: eventType='%s', data=%s", event.EventType, string(event.Data))

	var openaiChunk map[string]any
	if err := json.Unmarshal(event.Data, &openaiChunk); err != nil {
		logging.StreamDebugf("[OPENAI TRANSFORM] Failed to parse as OpenAI format, passing through unchanged: %v", err)
		return []transformer.SSEEvent{*event}, nil
	}

	logging.StreamDebugf("[OPENAI TRANSFORM] Parsed OpenAI chunk: %+v", openaiChunk)

	var result []transformer.SSEEvent

	// Check for finish_reason to detect stream completion
	if openaiChunk["choices"] != nil {
		choices, ok := openaiChunk["choices"].([]any)
		if ok && len(choices) > 0 {
			choice, ok := choices[0].(map[string]any)
			if ok {
				finishReason := choice["finish_reason"]
				logging.StreamDebugf("[OPENAI TRANSFORM] finish_reason=%v", finishReason)
				if finishReason != nil && finishReason != "" {
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
					logging.StreamDebugf("[OPENAI TRANSFORM] Generated content_block_stop event")

					usage, hasUsage := openaiChunk["usage"]
					if hasUsage {
						usageMap, ok := usage.(map[string]any)
						if ok {
							outputTokens, hasOutputTokens := usageMap["completion_tokens"]
							if hasOutputTokens {
								anthropicStopReason := t.mapStopReason(fmt.Sprintf("%v", finishReason))
								messageDelta := map[string]any{
									"type":  "message_delta",
									"delta": map[string]string{
										"stop_reason": anthropicStopReason,
									},
									"usage": map[string]any{
										"output_tokens": outputTokens,
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
								logging.StreamDebugf("[OPENAI TRANSFORM] Generated message_delta event with output_tokens=%v", outputTokens)
							}
						}
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
					logging.StreamDebugf("[OPENAI TRANSFORM] Generated message_stop event")
					t.resetToolCallStates()
					return result, nil
				}
			}
		}
	}

	// Extract content from delta
	if openaiChunk["choices"] != nil {
		choices, ok := openaiChunk["choices"].([]any)
		if ok && len(choices) > 0 {
			choice, ok := choices[0].(map[string]any)
			if ok {
				delta, ok := choice["delta"].(map[string]any)
				if ok {
					content, hasContent := delta["content"].(string)
					logging.StreamDebugf("[OPENAI TRANSFORM] delta found, content='%s', hasContent=%v", content, hasContent)
					if hasContent && content != "" {
						var events []transformer.SSEEvent
						if !t.messageStarted {
							msgStart, err := t.generateMessageStart()
							if err != nil {
								return nil, fmt.Errorf("failed to generate message_start: %w", err)
							}
							if len(msgStart.Data) > 0 {
								events = append(events, msgStart)
								t.messageStarted = true
								logging.StreamDebugf("[OPENAI TRANSFORM] Generated message_start event")
							}
						}

						anthropicDelta := map[string]any{
							"type":  "content_block_delta",
							"index": 0,
							"delta": map[string]string{
								"type": "text_delta",
								"text": content,
							},
						}
						data, err := json.Marshal(anthropicDelta)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
						}
						if len(data) == 0 {
							return nil, fmt.Errorf("content_block_delta event marshaled to empty JSON")
						}
						logging.StreamDebugf("[OPENAI TRANSFORM] Generated content_block_delta with text: '%s'", content)
						events = append(events, transformer.SSEEvent{
							EventType: "content_block_delta",
							Data:      data,
						})
						return events, nil
					}

					if toolCalls, hasToolCalls := delta["tool_calls"].([]any); hasToolCalls && len(toolCalls) > 0 {
						logging.StreamDebugf("[OPENAI TRANSFORM] Found tool_calls in delta, count=%d", len(toolCalls))

						var events []transformer.SSEEvent
						if !t.messageStarted {
							msgStart, err := t.generateMessageStart()
							if err != nil {
								return nil, fmt.Errorf("failed to generate message_start: %w", err)
							}
							if len(msgStart.Data) > 0 {
								events = append(events, msgStart)
								t.messageStarted = true
								logging.StreamDebugf("[OPENAI TRANSFORM] Generated message_start event before tool calls")
							}
						}

						toolCallEvents, err := t.transformToolCallChunks(toolCalls)
						if err != nil {
							return nil, err
						}
						events = append(events, toolCallEvents...)
						return events, nil
					}
				}
			}
		}
	}

	if openaiChunk["id"] != nil || openaiChunk["object"] != nil {
		if openaiChunk["choices"] == nil {
			logging.StreamDebugf("[OPENAI TRANSFORM] Filtering out OpenAI metadata-only chunk")
			return []transformer.SSEEvent{}, nil
		}
	}

	logging.StreamDebugf("[OPENAI TRANSFORM] Passing through unchanged (no choices/delta/finish_reason)")
	return []transformer.SSEEvent{*event}, nil
}
