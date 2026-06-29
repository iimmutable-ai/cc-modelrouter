// Package transformer provides the base transformer implementation.
package transformer

import (
	"encoding/json"
	"fmt"

	"github.com/iimmutable-ai/cc-modelrouter/internal/useragent"
)

// BaseTransformer provides common utilities for transformers.
type BaseTransformer struct {
	name      string
	userAgent string
}

// NewBaseTransformer creates a new base transformer.
func NewBaseTransformer(name string) *BaseTransformer {
	return &BaseTransformer{name: name, userAgent: useragent.Default()}
}

// UserAgent returns the User-Agent header value this transformer sends to its
// provider. Defaults to the Claude Code SDK UA; override via SetUserAgent.
func (b *BaseTransformer) UserAgent() string {
	if b.userAgent != "" {
		return b.userAgent
	}
	return useragent.Default()
}

// SetUserAgent overrides the User-Agent header value sent on outbound requests.
func (b *BaseTransformer) SetUserAgent(ua string) {
	b.userAgent = ua
}

// Name returns the transformer name.
func (b *BaseTransformer) Name() string {
	return b.name
}

// MarshalSSEEvent creates an SSEEvent from the provided data.
func (b *BaseTransformer) MarshalSSEEvent(eventType string, data any) (SSEEvent, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return SSEEvent{}, fmt.Errorf("failed to marshal %s event: %w", eventType, err)
	}
	if len(jsonData) == 0 {
		return SSEEvent{}, fmt.Errorf("%s event marshaled to empty JSON", eventType)
	}
	return SSEEvent{EventType: eventType, Data: jsonData}, nil
}