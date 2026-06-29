package logging

import (
	"net/http"
	"strings"
	"testing"
)

func TestSanitizeHeaders(t *testing.T) {
	tests := []struct {
		name            string
		headers         http.Header
		sensitiveKeys   []string // Headers that should be redacted
		plaintextKeys   []string // Headers that should NOT be redacted
	}{
		{
			name: "api_key_redacted",
			headers: http.Header{
				"X-Api-Key":           {"sk-secret-key-12345"},
				"Content-Type":        {"application/json"},
				"Anthropic-Version":   {"2023-06-01"},
				"User-Agent":          {"test-agent"},
			},
			sensitiveKeys: []string{"X-Api-Key"},
			plaintextKeys: []string{"Content-Type", "Anthropic-Version", "User-Agent"},
		},
		{
			name: "authorization_redacted",
			headers: http.Header{
				"Authorization": {"Bearer super-secret-token"},
				"Accept":        {"application/json"},
			},
			sensitiveKeys: []string{"Authorization"},
			plaintextKeys: []string{"Accept"},
		},
		{
			name: "multiple_secrets",
			headers: http.Header{
				"X-Api-Key":     {"sk-key-1"},
				"Authorization": {"Bearer token-2"},
				"Cookie":        {"session=abc123"},
				"Content-Type":  {"text/plain"},
			},
			sensitiveKeys: []string{"X-Api-Key", "Authorization", "Cookie"},
			plaintextKeys: []string{"Content-Type"},
		},
		{
			name: "case_insensitive",
			headers: http.Header{
				"X-API-KEY":       {"secret-key"},
				"x-api-key":       {"another-secret"},
				"AUTHORIZATION":   {"Bearer token"},
			},
			sensitiveKeys: []string{"X-API-KEY", "x-api-key", "AUTHORIZATION"},
			plaintextKeys: nil,
		},
		{
			name:            "empty_headers",
			headers:         http.Header{},
			sensitiveKeys:   nil,
			plaintextKeys:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizeHeaders(tt.headers)

			// Verify sensitive headers are redacted
			for _, key := range tt.sensitiveKeys {
				values := sanitized[key]
				if len(values) == 0 {
					continue // Header not set
				}
				for _, v := range values {
					// Check original value is NOT present
					original := tt.headers[key]
					for _, orig := range original {
						if strings.Contains(v, orig) && len(orig) > 0 {
							t.Errorf("Sensitive header %s leaked value: %s", key, v)
						}
					}
					// Check redaction marker is present
					if !strings.Contains(v, "REDACTED") && !strings.Contains(v, "EMPTY") {
						t.Errorf("Header %s not marked as redacted: %s", key, v)
					}
				}
			}

			// Verify non-sensitive headers are preserved
			for _, key := range tt.plaintextKeys {
				if tt.headers[key] == nil {
					continue
				}
				expected := tt.headers[key]
				actual := sanitized[key]
				if len(expected) != len(actual) {
					t.Errorf("Header %s values count mismatch: expected %d, got %d", key, len(expected), len(actual))
					continue
				}
				for i, v := range expected {
					if actual[i] != v {
						t.Errorf("Non-sensitive header %s modified: expected %s, got %s", key, v, actual[i])
					}
				}
			}
		})
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		mustNotContain string // The original value must NOT appear in output
		mustContain    string // Output must contain this marker
	}{
		{
			name:           "long_key",
			input:          "sk-secret-key-very-long-1234567890",
			mustNotContain: "sk-secret-key-very-long-1234567890",
			mustContain:    "REDACTED",
		},
		{
			name:           "medium_key",
			input:          "sk-12345678",
			mustNotContain: "sk-12345678",
			mustContain:    "REDACTED",
		},
		{
			name:           "short_key",
			input:          "short",
			mustNotContain: "short",
			mustContain:    "REDACTED",
		},
		{
			name:           "empty",
			input:          "",
			mustNotContain: "",
			mustContain:    "EMPTY",
		},
		{
			name:           "bearer_token",
			input:          "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			mustNotContain: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			mustContain:    "REDACTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactValue(tt.input)

			// Verify original value not in output (except for empty)
			if tt.input != "" && strings.Contains(got, tt.input) {
				t.Errorf("Original value leaked in output: %s", got)
			}

			// Verify contains required marker
			if !strings.Contains(got, tt.mustContain) {
				t.Errorf("Output missing required marker %q: %s", tt.mustContain, got)
			}
		})
	}
}

func TestSanitizeHeadersString(t *testing.T) {
	tests := []struct {
		name          string
		headers       http.Header
		mustNotContain []string // These strings must NOT appear
		mustContain    []string // These strings must appear
	}{
		{
			name: "redacts_secrets",
			headers: http.Header{
				"X-Api-Key":    {"sk-secret-12345"},
				"Content-Type": {"application/json"},
			},
			mustNotContain: []string{"sk-secret-12345"},
			mustContain:    []string{"REDACTED", "Content-Type", "application/json"},
		},
		{
			name: "handles_nil",
			headers: nil,
			mustNotContain: nil,
			mustContain:    []string{"<nil>"},
		},
		{
			name: "preserves_safe_headers",
			headers: http.Header{
				"User-Agent":          {"test-router/9.9"},
				"Anthropic-Version":   {"2023-06-01"},
			},
			mustNotContain: []string{},
			mustContain:    []string{"User-Agent", "test-router/9.9", "Anthropic-Version", "2023-06-01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHeadersString(tt.headers)

			// Check forbidden strings
			for _, forbidden := range tt.mustNotContain {
				if strings.Contains(got, forbidden) {
					t.Errorf("Output contains forbidden string %q: %s", forbidden, got)
				}
			}

			// Check required strings
			for _, required := range tt.mustContain {
				if !strings.Contains(got, required) {
					t.Errorf("Output missing required string %q: %s", required, got)
				}
			}
		})
	}
}

func TestSensitiveHeaderVariants(t *testing.T) {
	// Test various case variations of sensitive headers
	variants := map[string]bool{
		"authorization":        true,
		"AUTHORIZATION":        true,
		"Authorization":        true,
		"x-api-key":           true,
		"X-API-KEY":           true,
		"X-Api-Key":           true,
		"cookie":              true,
		"COOKIE":              true,
		"Cookie":              true,
		"x-bigmodel-api-key":  true,
		"X-BigModel-Api-Key":  true,
	}

	for header, shouldBeSensitive := range variants {
		t.Run(header, func(t *testing.T) {
			lowerHeader := strings.ToLower(header)
			isSensitive := SensitiveHeaders[lowerHeader]
			if isSensitive != shouldBeSensitive {
				t.Errorf("Header %q: expected sensitive=%v, got %v", header, shouldBeSensitive, isSensitive)
			}
		})
	}
}