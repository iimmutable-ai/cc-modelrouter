//go:build error_tests
// +build error_tests

package error

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestMalformedJSONResponse tests handling of various malformed JSON responses.
func TestMalformedJSONResponse(t *testing.T) {
	testCases := []struct {
		name          string
		responseBody  string
		shouldError   bool
		description   string
	}{
		{
			name:         "unclosed_brace",
			responseBody: `{"id":"test","type":"message"`,
			shouldError:  true,
			description:  "JSON missing closing brace",
		},
		{
			name:         "unclosed_string",
			responseBody: `{"id":"test","content":"hello`,
			shouldError:  true,
			description:  "JSON string not closed",
		},
		{
			name:         "missing_quotes",
			responseBody: `{id:test,type:message}`,
			shouldError:  true,
			description:  "JSON keys without quotes",
		},
		{
			name:         "trailing_comma",
			responseBody: `{"id":"test","content":"hello",}`,
			shouldError:  true,
			description:  "JSON with trailing comma",
		},
		{
			name:         "invalid_escape",
			responseBody: `{"id":"test","content":"hello\x00world"}`,
			shouldError:  true,
			description:  "JSON with invalid escape sequence",
		},
		{
			name:         "empty_array_key",
			responseBody: `{"":[],"id":"test"}`,
			shouldError:  true,
			description:  "JSON with empty object key",
		},
		{
			name:         "nested_malformed",
			responseBody: `{"content":{"text":}}}`,
			shouldError:  true,
			description:  "JSON with nested malformed object",
		},
		{
			name:         "wrong_type_in_array",
			responseBody: `{"content":["string",{"valid":true}]}`,
			shouldError:  true,
			description:  "JSON array with mixed types",
		},
		{
			name:         "null_in_required_field",
			responseBody: `{"id":null,"type":"message"}`,
			shouldError:  true,
			description:  "JSON with null in required field",
		},
		{
			name:         "unicode_invalid",
			responseBody: `{"id":"test\ufffecontent":"hello"}`,
			shouldError:  true,
			description:  "JSON with invalid Unicode",
		},
		{
			name:         "deeply_nested_invalid",
			responseBody: `{"a":{"b":{"c":{"d":"e"}}}}`,
			shouldError:  false,
			description:  "Valid JSON but wrong structure",
		},
		{
			name:         "duplicate_keys",
			responseBody: `{"id":"test1","id":"test2"}`,
			shouldError:  true,
			description:  "JSON with duplicate keys",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			malformedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.responseBody))
			}))
			defer malformedServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("malformed", malformedServer.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "malformed:test-model").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         malformedServer.URL,
				APIKey:          "test-key",
				Timeout:         "5s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"malformed": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-malformed-" + tc.name)

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if tc.shouldError && w.Code == http.StatusOK {
				t.Errorf("Test %s (%s): expected error, got success", tc.name, tc.description)
			} else if !tc.shouldError && w.Code != http.StatusOK {
				t.Logf("Test %s (%s): got status %d, body: %s", tc.name, tc.description, w.Code, w.Body.String())
			}
		})
	}
}

// TestUnexpectedFormat tests handling of responses with unexpected structure.
func TestUnexpectedFormat(t *testing.T) {
	testCases := []struct {
		name         string
		responseBody string
		contentType  string
		description  string
	}{
		{
			name:         "plain_text",
			responseBody: "This is just plain text",
			contentType:  "text/plain",
			description:  "Plain text instead of JSON",
		},
		{
			name:         "html_response",
			responseBody: "<html><body>Error</body></html>",
			contentType:  "text/html",
			description:  "HTML error page",
		},
		{
			name:         "xml_response",
			responseBody: `<?xml version="1.0"?><error>Not found</error>`,
			contentType:  "application/xml",
			description:  "XML response instead of JSON",
		},
		{
			name:         "empty_body",
			responseBody: "",
			contentType:  "application/json",
			description:  "Empty response body",
		},
		{
			name:         "whitespace_only",
			responseBody: "   \n\t   ",
			contentType:  "application/json",
			description:  "Only whitespace",
		},
		{
			name:         "binary_data",
			responseBody: "\x00\x01\x02\x03\x04",
			contentType:  "application/octet-stream",
			description:  "Binary data",
		},
		{
			name:         "wrong_json_version",
			responseBody: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"}}`,
			contentType:  "application/json",
			description:  "JSON-RPC format",
		},
		{
			name:         "array_response",
			responseBody: `["item1","item2"]`,
			contentType:  "application/json",
			description:  "JSON array instead of object",
		},
		{
			name:         "number_response",
			responseBody: `12345`,
			contentType:  "application/json",
			description:  "JSON number instead of object",
		},
		{
			name:         "boolean_response",
			responseBody: `true`,
			contentType:  "application/json",
			description:  "JSON boolean instead of object",
		},
		{
			name:         "form_urlencoded",
			responseBody: "error=not_found&code=404",
			contentType:  "application/x-www-form-urlencoded",
			description:  "Form-encoded data",
		},
		{
			name:         "multipart_form",
			responseBody: "--boundary\r\nContent-Disposition: form-data\r\n\r\nerror\r\n--boundary--",
			contentType:  "multipart/form-data",
			description:  "Multipart form data",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			formatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Write([]byte(tc.responseBody))
			}))
			defer formatServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("format-test", formatServer.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "format-test:test-model").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         formatServer.URL,
				APIKey:          "test-key",
				Timeout:         "5s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"format-test": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-format-" + tc.name)

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			t.Logf("Test %s (%s): status=%d", tc.name, tc.description, w.Code)
		})
	}
}

// TestInvalidSSEEvents tests handling of malformed SSE events during streaming.
func TestInvalidSSEEvents(t *testing.T) {
	testCases := []struct {
		name        string
		sseData     string
		description string
	}{
		{
			name:        "missing_event_prefix",
			sseData:     `{"type":"message_start"}\n\n`,
			description: "SSE data without event: prefix",
		},
		{
			name:        "malformed_data",
			sseData:     `event: message_start\ndata: {invalid json}\n\n`,
			description: "Invalid JSON in SSE data",
		},
		{
			name:        "empty_event",
			sseData:     `event: \ndata: \n\n`,
			description: "Empty SSE event",
		},
		{
			name:        "missing_newline",
			sseData:     `event: message_startdata: {"type":"message_start"}`,
			description: "SSE without proper line breaks",
		},
		{
			name:        "double_event",
			sseData:     `event: message_start\nevent: content_block\ndata: {"test":"value"}\n\n`,
			description: "Two event types without data between",
		},
		{
			name:        "no_final_newline",
			sseData:     `event: message_start\ndata: {"type":"message_start"}`,
			description: "SSE event without final double newline",
		},
		{
			name:        "comment_only",
			sseData:     `: this is a comment\nevent: message_start\ndata: {"type":"message_start"}`,
			description: "SSE with comment line",
		},
		{
			name:        "id_without_event",
			sseData:     `id: msg-123\ndata: {"type":"message_start"}`,
			description: "SSE with id but no event type",
		},
		{
			name:        "retry_value",
			sseData:     `retry: 3000\ndata: {"type":"message_start"}`,
			description: "SSE with retry directive",
		},
		{
			name:        "large_data_chunk",
			sseData:     `event: message_start\ndata: "` + string(make([]byte, 10000)) + `\n\n`,
			description: "Very large SSE data chunk",
		},
		{
			name:        "unicode_in_event",
			sseData:     `event: message_start\ndata: {"text":"你好 世界 🌍"}\n\n`,
			description: "Unicode characters in SSE",
		},
		{
			name:        "null_byte_in_stream",
			sseData:     `event: message_start\ndata: {"text":"\x00test"}\n\n`,
			description: "Null byte in SSE stream",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte(tc.sseData))
			}))
			defer sseServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("sse-test", sseServer.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "sse-test:test-model").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         sseServer.URL,
				APIKey:          "test-key",
				Timeout:         "5s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"sse-test": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-sse-" + tc.name)

			reqBody := map[string]any{
				"model":    "test-model",
				"stream":   true,
				"messages": []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			t.Logf("Test %s (%s): status=%d", tc.name, tc.description, w.Code)
		})
	}
}

// TestMissingRequiredFields tests responses missing required fields.
func TestMissingRequiredFields(t *testing.T) {
	testCases := []struct {
		name         string
		responseBody string
		missingField string
		description  string
	}{
		{
			name:         "missing_id",
			responseBody: `{"type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			missingField: "id",
			description:  "Response missing id field",
		},
		{
			name:         "missing_type",
			responseBody: `{"id":"test-id","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			missingField: "type",
			description:  "Response missing type field",
		},
		{
			name:         "missing_content",
			responseBody: `{"id":"test-id","type":"message","role":"assistant","model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			missingField: "content",
			description:  "Response missing content field",
		},
		{
			name:         "missing_model",
			responseBody: `{"id":"test-id","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			missingField: "model",
			description:  "Response missing model field",
		},
		{
			name:         "missing_usage",
			responseBody: `{"id":"test-id","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"test-model","stop_reason":"end_turn"}`,
			missingField: "usage",
			description:  "Response missing usage field",
		},
		{
			name:         "empty_response",
			responseBody: `{}`,
			missingField: "all",
			description:  "Empty JSON object",
		},
		{
			name:         "null_required_fields",
			responseBody: `{"id":null,"type":null,"content":null,"model":null,"usage":null}`,
			missingField: "all",
			description:  "All required fields are null",
		},
		{
			name:         "wrong_field_types",
			responseBody: `{"id":123,"type":456,"content":"not array","model":789,"usage":"not object"}`,
			missingField: "types",
			description:  "All fields have wrong types",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			missingFieldsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.responseBody))
			}))
			defer missingFieldsServer.Close()

			cfg := util.NewTestConfigBuilder().
				WithServer("localhost", 18081).
				WithProvider("missing-fields", missingFieldsServer.URL, "test-key", []string{"test-model"}, "anthropic").
				WithRoute("test-model", "missing-fields:test-model").
				Build()

			client, _ := provider.NewClient(&provider.ClientConfig{
				BaseURL:         missingFieldsServer.URL,
				APIKey:          "test-key",
				Timeout:         "5s",
				MaxIdleConns:    100,
				IdleConnTimeout: "90s",
			})

			registry := transformer.NewRegistry()
			registry.Register(providers.NewAnthropicTransformer())

			routerEngine := router.NewEngine(cfg)

			handler := proxy.NewHandler(50 * 1024 * 1024)
			handler.SetRouter(&util.RouterAdapter{Engine: routerEngine})
			handler.SetTransformerRegistry(&util.RegistryAdapter{Registry: registry})
			handler.SetProviderClients(map[string]proxy.HTTPClient{"missing-fields": client})
			handler.SetConfig(cfg)
			handler.SetInstanceID("test-missing-" + tc.name)

			reqBody := map[string]any{
				"model":      "test-model",
				"max_tokens": 100,
				"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			t.Logf("Test %s (missing %s): status=%d", tc.name, tc.missingField, w.Code)
		})
	}
}