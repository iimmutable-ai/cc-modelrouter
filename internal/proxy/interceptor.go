// Package proxy implements interceptor interfaces for request/response processing.
package proxy

import (
	"context"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// Context keys for multi-user request flow.
type contextKey int

const (
	// CtxKeyRawToken carries the raw Bearer token from the Authorization header.
	CtxKeyRawToken contextKey = iota
	// CtxKeyUser carries the resolved UserInfo after authentication.
	CtxKeyUser
)

// UserInfoFromContext extracts the UserInfo from context, if present.
func UserInfoFromContext(ctx context.Context) *auth.UserInfo {
	if u, ok := ctx.Value(CtxKeyUser).(*auth.UserInfo); ok {
		return u
	}
	return nil
}

// RequestInterceptor intercepts and can modify requests before they are processed.
type RequestInterceptor interface {
	// InterceptRequest is called before the request is sent to the provider.
	// It can modify the request, log details, or return an error to prevent processing.
	InterceptRequest(ctx context.Context, req *anthropic.Request) error
}

// ResponseInterceptor intercepts responses before they are returned to the client.
type ResponseInterceptor interface {
	// InterceptResponse is called after the provider responds but before
	// the response is sent to the client.
	InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error
}

// StreamingResponseInterceptor intercepts streaming responses.
type StreamingResponseInterceptor interface {
	// InterceptStreamingEvent is called for each SSE event before it is sent to the client.
	InterceptStreamingEvent(ctx context.Context, req *anthropic.Request, eventType string, data []byte) ([]byte, error)
}

// LoggingInterceptor logs request and response details.
type LoggingInterceptor struct {
	LogRequestDetails  bool
	LogResponseDetails bool
}

// NewLoggingInterceptor creates a new logging interceptor.
func NewLoggingInterceptor() *LoggingInterceptor {
	return &LoggingInterceptor{
		LogRequestDetails:  true,
		LogResponseDetails: true,
	}
}

// InterceptRequest logs request details.
func (l *LoggingInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	if !l.LogRequestDetails {
		return nil
	}

	logging.Debugf("Request: Model=%s, MaxTokens=%d, Stream=%v, Messages=%d",
		req.Model, req.MaxTokens, req.Stream, len(req.Messages))

	if len(req.Tools) > 0 {
		logging.Debugf("Request Tools: %d", len(req.Tools))
		for i, tool := range req.Tools {
			logging.Debugf("  Tool[%d]: %s", i, tool.Name)
		}
	}

	if req.Thinking != nil {
		logging.Debugf("Request Thinking: Type=%s, BudgetTokens=%d",
			req.Thinking.Type, req.Thinking.BudgetTokens)
	}

	return nil
}

// InterceptResponse logs response details.
func (l *LoggingInterceptor) InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error {
	if !l.LogResponseDetails {
		return nil
	}

	logging.Debugf("Response: ID=%s, Model=%s, Type=%s, StopReason=%s",
		resp.ID, resp.Model, resp.Type, resp.StopReason)
	logging.Debugf("Response Usage: InputTokens=%d, OutputTokens=%d",
		resp.Usage.InputTokens, resp.Usage.OutputTokens)

	return nil
}

// MetricsInterceptor tracks request/response metrics.
type MetricsInterceptor struct {
	RequestCount  int
	ResponseCount int
	ErrorCount    int
	StartTime     time.Time
}

// NewMetricsInterceptor creates a new metrics interceptor.
func NewMetricsInterceptor() *MetricsInterceptor {
	return &MetricsInterceptor{
		StartTime: time.Now(),
	}
}

// InterceptRequest increments request counter.
func (m *MetricsInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	m.RequestCount++
	return nil
}

// InterceptResponse increments response counter.
func (m *MetricsInterceptor) InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error {
	m.ResponseCount++
	return nil
}

// GetMetrics returns current metrics.
func (m *MetricsInterceptor) GetMetrics() (requestCount, responseCount, errorCount int, uptime time.Duration) {
	return m.RequestCount, m.ResponseCount, m.ErrorCount, time.Since(m.StartTime)
}

// TimingInterceptor measures request processing time.
type TimingInterceptor struct {
	requestStarts map[interface{}]time.Time
}

// NewTimingInterceptor creates a new timing interceptor.
func NewTimingInterceptor() *TimingInterceptor {
	return &TimingInterceptor{
		requestStarts: make(map[interface{}]time.Time),
	}
}

// InterceptRequest records the start time.
func (t *TimingInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	t.requestStarts[req] = time.Now()
	return nil
}

// InterceptResponse logs the elapsed time.
func (t *TimingInterceptor) InterceptResponse(ctx context.Context, req *anthropic.Request, resp *anthropic.Response) error {
	if start, ok := t.requestStarts[req]; ok {
		elapsed := time.Since(start)
		logging.Infof("Request completed in %v", elapsed)
		delete(t.requestStarts, req)
	}
	return nil
}
