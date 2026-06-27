package proxy

import (
	"net/http"

	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
)

// QoSInterceptor wraps the QoS engine for use as a request gate.
// It is NOT a standard RequestInterceptor — it must run before
// the interceptor chain in ServeHTTP because it controls admission.
type QoSInterceptor struct {
	Engine *qos.QoSEngine
}

// NewQoSInterceptor creates a new QoS interceptor.
func NewQoSInterceptor(engine *qos.QoSEngine) *QoSInterceptor {
	return &QoSInterceptor{Engine: engine}
}

// WriteQoSRejected writes a 429 response when QoS rejects a request.
func WriteQoSRejected(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"` + message + `"}}`))
}
