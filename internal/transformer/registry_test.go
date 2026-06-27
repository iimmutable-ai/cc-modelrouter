package transformer

import (
	"net/http"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	// Register a mock transformer
	mock := &mockTransformer{name: "test"}
	reg.Register(mock)

	// Retrieve it
	got, err := reg.Get("test")
	if err != nil {
		t.Fatalf("failed to get transformer: %v", err)
	}

	if got.Name() != "test" {
		t.Errorf("expected name 'test', got '%s'", got.Name())
	}

	// Test not found
	_, err = reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent transformer")
	}
}

func TestRegistryHas(t *testing.T) {
	reg := NewRegistry()

	// Test empty registry
	if reg.Has("test") {
		t.Error("expected Has to return false for empty registry")
	}

	// Register a transformer
	mock := &mockTransformer{name: "test"}
	reg.Register(mock)

	// Test existing transformer
	if !reg.Has("test") {
		t.Error("expected Has to return true for registered transformer")
	}

	// Test non-existent transformer
	if reg.Has("nonexistent") {
		t.Error("expected Has to return false for non-existent transformer")
	}
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry()

	// Test empty registry
	names := reg.Names()
	if len(names) != 0 {
		t.Errorf("expected empty names slice, got %d", len(names))
	}

	// Register multiple transformers
	reg.Register(&mockTransformer{name: "test1"})
	reg.Register(&mockTransformer{name: "test2"})
	reg.Register(&mockTransformer{name: "test3"})

	// Test names
	names = reg.Names()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}

	// Verify all names are present
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range []string{"test1", "test2", "test3"} {
		if !nameSet[expected] {
			t.Errorf("expected name '%s' not found in Names()", expected)
		}
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	// Register initial transformers
	for i := 0; i < 10; i++ {
		reg.Register(&mockTransformer{name: string(rune('a' + i))})
	}

	// Concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			reg.Register(&mockTransformer{name: "concurrent"})
		}
		done <- true
	}()

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = reg.Has("a")
				_ = reg.Names()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}
}

// mockTransformer for testing
type mockTransformer struct {
	name string
}

func (m *mockTransformer) Name() string { return m.name }

func (m *mockTransformer) Endpoint() string { return "/" }

func (m *mockTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	return nil, nil
}

func (m *mockTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	return nil, nil
}

func (m *mockTransformer) SupportsStreaming() bool { return false }

func (m *mockTransformer) TransformStreamEvent(event *SSEEvent) ([]SSEEvent, error) {
	return nil, nil
}
