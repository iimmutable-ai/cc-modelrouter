// Package transformer defines the transformer interface for request/response transformation.
package transformer

import (
	"net/http"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// SSEEvent represents a complete server-sent event with type and data.
type SSEEvent struct {
	// EventType is the SSE event type (e.g., "message_start", "content_block_delta").
	EventType string
	// Data is the raw JSON data payload.
	Data []byte
}

// Provider represents a provider configuration (deprecated, for backward compatibility).
type Provider struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Transformer transforms between Anthropic and provider formats.
type Transformer interface {
	// Name returns the transformer name.
	Name() string

	// Endpoint returns the API endpoint path.
	Endpoint() string

	// PrepareRequest converts Anthropic request to provider HTTP request.
	PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error)

	// ParseResponse converts provider HTTP response to Anthropic response.
	ParseResponse(resp *http.Response) (*anthropic.Response, error)

	// SupportsStreaming returns true if transformer supports streaming.
	SupportsStreaming() bool

	// TransformStreamEvent converts provider SSE event to Anthropic SSE events.
	TransformStreamEvent(event *SSEEvent) ([]SSEEvent, error)
}
