package provider_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
)

// TestRepro_ProviderClientRetry tests if the HTTP client properly handles
// body restoration during retries with the actual retry logic.
func TestRepro_ProviderClientRetry(t *testing.T) {
	attempt := 0

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++

		// Try to read body
		var bodyLen int
		if r.GetBody != nil {
			if reader, err := r.GetBody(); err == nil && reader != nil {
				body, _ := io.ReadAll(reader)
				reader.Close()
				bodyLen = len(body)
			}
		}
		if bodyLen == 0 && r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			bodyLen = len(body)
		}

		t.Logf("[MOCK] Attempt #%d: Body length: %d", attempt, bodyLen)

		// Fail first 2 attempts
		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"retry"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test"}`))
	}))
	defer mockServer.Close()

	// Create client with retries
	client, err := provider.NewClient(&provider.ClientConfig{
		BaseURL:    mockServer.URL,
		APIKey:    "test",
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create request with proper GetBody
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`)
	req, _ := http.NewRequest("POST", mockServer.URL, nil)
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute
	resp, err := client.Do(req)

	t.Logf("Total attempts: %d, Final error: %v", attempt, err)

	if attempt < 3 {
		t.Errorf("Expected 3 attempts, got %d", attempt)
	}

	if resp != nil {
		resp.Body.Close()
	}
}

// TestRepro_StreamingClientRetry tests retry with streaming client
func TestRepro_StreamingClientRetry(t *testing.T) {
	attempt := 0

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++

		var bodyLen int
		if r.GetBody != nil {
			if reader, err := r.GetBody(); err == nil && reader != nil {
				body, _ := io.ReadAll(reader)
				reader.Close()
				bodyLen = len(body)
			}
		}

		t.Logf("[MOCK] Streaming attempt #%d: Body length: %d", attempt, bodyLen)

		if attempt < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer mockServer.Close()

	client, err := provider.NewStreamingClient(&provider.ClientConfig{
		BaseURL:    mockServer.URL,
		APIKey:    "test",
		MaxRetries: 2,
		RetryDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	body := []byte(`{"model":"test","stream":true}`)
	req, _ := http.NewRequest("POST", mockServer.URL, nil)
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.Header.Set("Content-Type", "application/json")

	ctx := context.Background()
	resp, err := client.DoWithContext(ctx, req)

	t.Logf("Streaming total attempts: %d", attempt)

	if resp != nil {
		resp.Body.Close()
	}
}