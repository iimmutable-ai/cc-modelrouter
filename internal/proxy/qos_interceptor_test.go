package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
)

func TestNewQoSInterceptor(t *testing.T) {
	engine := qos.NewQoSEngine(10, qos.WREDConfig{}, nil)
	qi := NewQoSInterceptor(engine)
	if qi == nil {
		t.Fatal("NewQoSInterceptor returned nil")
	}
	if qi.Engine != engine {
		t.Error("Engine not set")
	}
}

func TestWriteQoSRejected_ResponseFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteQoSRejected(rec, "rate limit exceeded")

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body["type"] != "error" {
		t.Errorf("expected type 'error', got %q", body["type"])
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["type"] != "overloaded_error" {
		t.Errorf("expected error.type 'overloaded_error', got %q", errObj["type"])
	}
	if errObj["message"] != "rate limit exceeded" {
		t.Errorf("expected error.message 'rate limit exceeded', got %q", errObj["message"])
	}
}

func TestWriteQoSRejected_DifferentMessages(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{"concurrency", "max concurrency reached"},
		{"capacity", "provider at capacity"},
		{"backoff", "backoff in effect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteQoSRejected(rec, tt.message)
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("expected 429, got %d", rec.Code)
			}
			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("parse JSON: %v", err)
			}
			errObj := body["error"].(map[string]interface{})
			if errObj["message"] != tt.message {
				t.Errorf("expected message %q, got %q", tt.message, errObj["message"])
			}
		})
	}
}
