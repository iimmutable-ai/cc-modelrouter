// Package security_test contains tests to verify secrets are never logged.
package security_test

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
)

// Test_NoAPIKeyInLogs verifies API keys are never logged in plaintext.
// This is a CRITICAL security test - it must never fail.
func Test_NoAPIKeyInLogs(t *testing.T) {
	// Test that the sanitization function properly redacts secrets
	t.Run("sanitize_headers", func(t *testing.T) {
		req := httptest.NewRequest("POST", "https://api.example.com/test", nil)
		req.Header.Set("X-Api-Key", "sk-secret-key-12345")
		req.Header.Set("Authorization", "Bearer secret-token-67890")
		req.Header.Set("Content-Type", "application/json")

		// Get sanitized output
		sanitized := logging.SanitizeHeadersString(req.Header)

		// CRITICAL: Verify secrets are NOT in output
		if strings.Contains(sanitized, "sk-secret-key-12345") {
			t.Errorf("SECURITY FAIL: X-Api-Key leaked in sanitized output: %s", sanitized)
			t.FailNow()
		}

		if strings.Contains(sanitized, "secret-token-67890") {
			t.Errorf("SECURITY FAIL: Authorization token leaked in sanitized output: %s", sanitized)
			t.FailNow()
		}

		// Verify headers ARE logged (but redacted)
		if !strings.Contains(sanitized, "X-Api-Key") {
			t.Log("Warning: X-Api-Key header not logged at all")
		}

		if !strings.Contains(sanitized, "REDACTED") {
			t.Errorf("SECURITY FAIL: Headers logged without redaction marker: %s", sanitized)
			t.FailNow()
		}

		t.Logf("PASS: Secrets properly redacted: %s", sanitized)
	})
}

// Test_HeaderRedaction verifies all sensitive headers are redacted.
func Test_HeaderRedaction(t *testing.T) {
	sensitiveHeaders := []struct {
		name  string
		value string
	}{
		{"X-Api-Key", "sk-very-secret-key-12345"},
		{"Authorization", "Bearer super-secret-token"},
		{"X-Auth-Token", "auth-token-xyz"},
		{"Cookie", "session=abc123; token=def456"},
		{"Set-Cookie", "session=ghi789"},
		{"Proxy-Authorization", "Basic dXNlcjpwYXNz"},
		{"X-BigModel-Api-Key", "bigmodel-secret-key"},
	}

	for _, tc := range sensitiveHeaders {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://test.com", nil)
			req.Header.Set(tc.name, tc.value)
			req.Header.Set("Content-Type", "application/json") // Non-sensitive

			sanitized := logging.SanitizeHeadersString(req.Header)

			// Verify secret value not in output
			if strings.Contains(sanitized, tc.value) {
				t.Errorf("SECURITY FAIL: %s value leaked: %s", tc.name, sanitized)
			}

			// Verify header name is still present (redacted)
			// Use case-insensitive check since Go canonicalizes header keys
			if !strings.Contains(strings.ToLower(sanitized), strings.ToLower(tc.name)) {
				t.Errorf("Header %s missing entirely from output: %s", tc.name, sanitized)
			}

			// Verify redaction marker
			if !strings.Contains(sanitized, "REDACTED") {
				t.Errorf("No redaction marker for %s: %s", tc.name, sanitized)
			}

			// Verify non-sensitive header preserved
			if !strings.Contains(sanitized, "application/json") {
				t.Errorf("Non-sensitive header Content-Type not preserved")
			}
		})
	}
}

// Test_LogOutputCapture tests that logging with sanitized headers doesn't leak secrets.
func Test_LogOutputCapture(t *testing.T) {
	// Capture log output
	var logBuf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOutput)

	// Simulate logging a request with sensitive headers
	req := httptest.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("X-Api-Key", "sk-ant-api03-secret-key-1234567890")
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	// Log with sanitized headers (this is what the fixed code should do)
	log.Printf("[TEST REQUEST] URL: %s, Method: %s, Headers: %s",
		req.URL.String(), req.Method, logging.SanitizeHeadersString(req.Header))

	logOutput := logBuf.String()

	// CRITICAL: Verify secrets are NOT in log output
	if strings.Contains(logOutput, "sk-ant-api03-secret-key-1234567890") {
		t.Errorf("SECURITY FAIL: X-Api-Key leaked in log output:\n%s", logOutput)
		t.FailNow()
	}

	if strings.Contains(logOutput, "secret-token") {
		t.Errorf("SECURITY FAIL: Authorization token leaked in log output:\n%s", logOutput)
		t.FailNow()
	}

	// Verify redaction occurred
	if !strings.Contains(logOutput, "REDACTED") {
		t.Errorf("No redaction marker in log output:\n%s", logOutput)
	}

	t.Logf("PASS: Log output safe:\n%s", logOutput)
}

// Test_MultipleHeaderValues tests redaction with multiple header values.
func Test_MultipleHeaderValues(t *testing.T) {
	headers := http.Header{}
	headers.Add("Set-Cookie", "session=abc123; Path=/")
	headers.Add("Set-Cookie", "token=xyz789; Path=/")
	headers.Set("X-Api-Key", "secret-key")

	sanitized := logging.SanitizeHeadersString(headers)

	// Verify neither cookie value leaked
	if strings.Contains(sanitized, "abc123") || strings.Contains(sanitized, "xyz789") {
		t.Errorf("SECURITY FAIL: Cookie values leaked: %s", sanitized)
	}

	// Verify API key not leaked
	if strings.Contains(sanitized, "secret-key") {
		t.Errorf("SECURITY FAIL: API key leaked: %s", sanitized)
	}

	t.Logf("PASS: Multiple values properly redacted: %s", sanitized)
}

// Test_RealWorldScenarios tests common real-world header combinations.
func Test_RealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		name    string
		headers http.Header
	}{
		{
			name: "anthropic_request",
			headers: http.Header{
				"X-Api-Key":         {"sk-ant-api03-xxxxxxxxxxxx"},
				"Anthropic-Version": {"2023-06-01"},
				"Content-Type":      {"application/json"},
			},
		},
		{
			name: "openai_request",
			headers: http.Header{
				"Authorization": {"Bearer sk-proj-xxxxxxxxxxxx"},
				"Content-Type":  {"application/json"},
			},
		},
		{
			name: "bigmodel_request",
			headers: http.Header{
				"X-BigModel-Api-Key": {"bigmodel-key-xxxx"},
				"Content-Type":       {"application/json"},
			},
		},
		{
			name: "authenticated_request",
			headers: http.Header{
				"Authorization": {"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
				"Cookie":        {"session_id=abc123xyz; user_token=secret"},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			sanitized := logging.SanitizeHeadersString(scenario.headers)

			// Check that no original secret values appear
			for key, values := range scenario.headers {
				lowerKey := strings.ToLower(key)
				if logging.SensitiveHeaders[lowerKey] {
					for _, val := range values {
						if len(val) > 8 && strings.Contains(sanitized, val) {
							t.Errorf("SECURITY FAIL: Secret value for %s leaked: %s", key, sanitized)
						}
					}
				}
			}

			t.Logf("PASS: %s - %s", scenario.name, sanitized)
		})
	}
}

// Test_NilAndEmptyHeaders tests edge cases.
func Test_NilAndEmptyHeaders(t *testing.T) {
	t.Run("nil_headers", func(t *testing.T) {
		result := logging.SanitizeHeadersString(nil)
		if result != "<nil>" {
			t.Errorf("Expected '<nil>', got: %s", result)
		}
	})

	t.Run("empty_headers", func(t *testing.T) {
		headers := http.Header{}
		result := logging.SanitizeHeadersString(headers)
		if !strings.HasPrefix(result, "map[") {
			t.Errorf("Unexpected format: %s", result)
		}
	})

	t.Run("empty_sensitive_header", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Api-Key", "")
		result := logging.SanitizeHeadersString(headers)
		if !strings.Contains(result, "EMPTY") {
			t.Errorf("Expected EMPTY marker for empty value: %s", result)
		}
	})
}

// captureOutput captures stdout/stderr during a test.
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}