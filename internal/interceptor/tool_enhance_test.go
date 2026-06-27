// Package interceptor tests for tool_enhance interceptor.
package interceptor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestNewToolEnhanceInterceptor(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config == nil {
		t.Error("expected config to be initialized")
	}
}

func TestNewToolEnhanceInterceptorWithConfig(t *testing.T) {
	config := &ToolEnhanceConfig{
		Enabled:             true,
		EnsureDescriptions:  false,
		NormalizeParameters: false,
		AddMissingType:      false,
	}

	interceptor := NewToolEnhanceInterceptorWithConfig(config)

	if interceptor == nil {
		t.Error("expected non-nil interceptor")
	}
	if interceptor.config != config {
		t.Error("expected config to be set")
	}
}

func TestDefaultToolEnhanceConfig(t *testing.T) {
	config := DefaultToolEnhanceConfig()

	if config == nil {
		t.Error("expected non-nil config")
	}
	if !config.Enabled {
		t.Error("expected Enabled to be true")
	}
	if !config.EnsureDescriptions {
		t.Error("expected EnsureDescriptions to be true")
	}
	if !config.NormalizeParameters {
		t.Error("expected NormalizeParameters to be true")
	}
	if !config.AddMissingType {
		t.Error("expected AddMissingType to be true")
	}
}

func TestToolEnhanceInterceptor_InterceptRequest(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name          string
		request       *anthropic.Request
		expectChanges bool
	}{
		{
			name: "Request with tools missing descriptions",
			request: &anthropic.Request{
				Model: "claude-3-5-sonnet",
				Tools: []anthropic.Tool{
					{Name: "search_web", InputSchema: map[string]any{"type": "object"}},
				},
			},
			expectChanges: true,
		},
		{
			name: "Request with tools having descriptions",
			request: &anthropic.Request{
				Model: "claude-3-5-sonnet",
				Tools: []anthropic.Tool{
					{Name: "search_web", Description: "Search the web", InputSchema: map[string]any{"type": "object"}},
				},
			},
			expectChanges: false,
		},
		{
			name: "Request without tools",
			request: &anthropic.Request{
				Model:  "claude-3-5-sonnet",
				Tools: []anthropic.Tool{},
			},
			expectChanges: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalTools := make([]anthropic.Tool, len(tt.request.Tools))
			for i, tool := range tt.request.Tools {
				originalTools[i] = tool
			}

			err := interceptor.InterceptRequest(context.Background(), tt.request)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check if tools were modified when expected
			if tt.expectChanges {
				changed := false
				for i, tool := range tt.request.Tools {
					if tool.Description != originalTools[i].Description {
						changed = true
						break
					}
				}
				if !changed {
					t.Error("expected tools to be modified")
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_generateDescription(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name          string
		toolName      string
		schema        any
		expectedDesc  string
	}{
		{
			name:         "Simple tool name",
			toolName:     "search",
			schema:       map[string]any{},
			expectedDesc: "Executes the search tool",
		},
		{
			name:         "Snake case tool name",
			toolName:     "search_web",
			schema:       map[string]any{},
			expectedDesc: "Search tool for Web",
		},
		{
			name:     "Description from schema",
			toolName: "search",
			schema: map[string]any{
				"description": "Search the web for information",
			},
			expectedDesc: "Search the web for information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.generateDescription(tt.toolName, tt.schema)
			if result != tt.expectedDesc {
				t.Errorf("expected %q, got %q", tt.expectedDesc, result)
			}
		})
	}
}

func TestToolEnhanceInterceptor_normalizeSchema(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name     string
		schema   any
		expected map[string]any
	}{
		{
			name: "Schema with type as array",
			schema: map[string]any{
				"type":     []any{"object"},
				"required": []any{"query"},
			},
			expected: map[string]any{
				"type":     "object",
				"required": []any{"query"},
			},
		},
		{
			name: "Schema with type as object with value",
			schema: map[string]any{
				"type": map[string]any{"value": "object"},
			},
			expected: map[string]any{
				"type": "object",
			},
		},
		{
			name: "Nested properties",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": []any{"string"},
					},
				},
			},
			expected: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.normalizeSchema(tt.schema)

			resultMap, ok := result.(map[string]any)
			if !ok {
				t.Fatal("expected result to be a map")
			}

			for k, expectedValue := range tt.expected {
				resultValue, ok := resultMap[k]
				if !ok {
					t.Errorf("expected key %s to exist", k)
					continue
				}

				// For slices, compare using JSON marshaling
				if expectedSlice, ok := expectedValue.([]any); ok {
					resultSlice, ok := resultValue.([]any)
					if !ok || len(resultSlice) != len(expectedSlice) {
						t.Errorf("expected %s=%v, got %v", k, expectedValue, resultValue)
						continue
					}
					for i := range expectedSlice {
						if resultSlice[i] != expectedSlice[i] {
							t.Errorf("expected %s[%d]=%v, got %v", k, i, expectedSlice[i], resultSlice[i])
						}
					}
				} else if expectedMap, ok := expectedValue.(map[string]any); ok {
					resultMapValue, ok := resultValue.(map[string]any)
					if !ok {
						t.Errorf("expected %s to be a map, got %T", k, resultValue)
						continue
					}
					// Recursively compare nested maps - convert to JSON for comparison
					expectedJSON, _ := json.Marshal(expectedMap)
					resultJSON, _ := json.Marshal(resultMapValue)
					if string(expectedJSON) != string(resultJSON) {
						t.Errorf("expected %s=%s, got %s", k, string(expectedJSON), string(resultJSON))
					}
				} else if resultValue != expectedValue {
					t.Errorf("expected %s=%v, got %v", k, expectedValue, resultValue)
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_addMissingTypeFields(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name     string
		schema   any
		expected string // type field value
	}{
		{
			name: "Object without type",
			schema: map[string]any{
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			expected: "object",
		},
		{
			name: "Object with type",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
			expected: "object",
		},
		{
			name: "String type",
			schema: map[string]any{
				"type": "string",
			},
			expected: "string",
		},
		{
			name: "Array without items type",
			schema: map[string]any{
				"type":  "array",
				"items": map[string]any{},
			},
			expected: "array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interceptor.addMissingTypeFields(tt.schema)

			resultMap, ok := result.(map[string]any)
			if !ok {
				t.Fatal("expected result to be a map")
			}

			if typeVal, ok := resultMap["type"]; !ok {
				t.Error("expected type field to exist")
			} else if typeVal != tt.expected {
				t.Errorf("expected type=%q, got %q", tt.expected, typeVal)
			}
		})
	}
}

func TestToolEnhanceInterceptor_ValidateToolSchema(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name        string
		schema      any
		expectError bool
	}{
		{
			name: "Valid object schema",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			expectError: false,
		},
		{
			name:        "Invalid schema (not a map)",
			schema:      "invalid",
			expectError: true,
		},
		{
			name:        "Schema missing type",
			schema:      map[string]any{"properties": map[string]any{}},
			expectError: true,
		},
		{
			name:        "Schema with type as object",
			schema:      map[string]any{"type": map[string]any{}},
			expectError: true,
		},
		{
			name: "Valid string schema",
			schema: map[string]any{
				"type": "string",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := interceptor.ValidateToolSchema(tt.schema)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_validateProperty(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name        string
		propName    string
		schema      any
		expectError bool
	}{
		{
			name:        "Valid string property",
			propName:    "query",
			schema:      map[string]any{"type": "string"},
			expectError: false,
		},
		{
			name:        "Valid object property",
			propName:    "options",
			schema:      map[string]any{"type": "object", "properties": map[string]any{}},
			expectError: false,
		},
		{
			name:        "Array with items",
			propName:    "items",
			schema:      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			expectError: false,
		},
		{
			name:        "Array without items",
			propName:    "items",
			schema:      map[string]any{"type": "array"},
			expectError: true,
		},
		{
			name:        "Property missing type",
			propName:    "value",
			schema:      map[string]any{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := interceptor.validateProperty(tt.propName, tt.schema)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_FixToolSchema(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name        string
		schema      any
		expectError bool
		checkType   string // expected type after fix
	}{
		{
			name: "Already valid schema",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			expectError: false,
			checkType:   "object",
		},
		{
			name: "Schema missing type",
			schema: map[string]any{
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			expectError: false,
			checkType:   "object",
		},
		{
			name: "Invalid schema that cannot be fixed",
			schema: map[string]any{
				"type": []any{"invalid"},
			},
			expectError: false,
			checkType:   "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interceptor.FixToolSchema(tt.schema)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				resultMap, ok := result.(map[string]any)
				if !ok {
					t.Fatal("expected result to be a map")
				}

				if typeVal, ok := resultMap["type"]; ok {
					if typeVal != tt.checkType {
						t.Errorf("expected type=%q, got %v", tt.checkType, typeVal)
					}
				} else if tt.checkType != "" {
					t.Error("expected type field to exist")
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_MergeToolSchemas(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	tests := []struct {
		name        string
		schemas     []any
		expectError bool
		checkProps  int // expected number of properties
	}{
		{
			name: "Merge two object schemas",
			schemas: []any{
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer"},
					},
				},
			},
			expectError: false,
			checkProps:  2,
		},
		{
			name:        "No schemas to merge",
			schemas:     []any{},
			expectError: true,
		},
		{
			name: "Single schema",
			schemas: []any{
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
			expectError: false,
			checkProps:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interceptor.MergeToolSchemas(tt.schemas)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				resultMap, ok := result.(map[string]any)
				if !ok {
					t.Fatal("expected result to be a map")
				}

				if props, ok := resultMap["properties"].(map[string]any); ok {
					if len(props) != tt.checkProps {
						t.Errorf("expected %d properties, got %d", tt.checkProps, len(props))
					}
				} else {
					t.Error("expected properties to exist")
				}
			}
		})
	}
}

func TestToolSchemaFixer_FixToolCallJSON(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()
	fixer := interceptor.ToolSchemaFixer()

	tests := []struct {
		name        string
		rawArgs     json.RawMessage
		expectError bool
		checkKey    string // expected key in result
	}{
		{
			name:     "Valid JSON",
			rawArgs:  json.RawMessage(`{"query":"test"}`),
			expectError: false,
			checkKey:  "query",
		},
		{
			name:     "JSON with trailing comma",
			rawArgs:  json.RawMessage(`{"query":"test",}`),
			expectError: false,
			checkKey:  "query",
		},
		{
			name:     "Simple string",
			rawArgs:  json.RawMessage(`test`),
			expectError: true,
			checkKey:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := fixer.FixToolCallJSON(tt.rawArgs)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				if tt.checkKey != "" {
					if _, ok := result[tt.checkKey]; !ok {
						t.Errorf("expected key %q in result", tt.checkKey)
					}
				}
			}
		})
	}
}

func TestToolSchemaFixer_tryFixJSON(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()
	fixer := interceptor.ToolSchemaFixer()

	tests := []struct {
		name        string
		jsonStr     string
		expectError bool
		expected    string
	}{
		{
			name:        "Remove trailing comma in object",
			jsonStr:     `{"key":"value",}`,
			expectError: false,
			expected:    `{"key":"value"}`,
		},
		{
			name:        "Remove trailing comma in array",
			jsonStr:     `[1,2,3,]`,
			expectError: false,
			expected:    `[1,2,3]`,
		},
		{
			name:        "Object with newline",
			jsonStr:     `{"key":"value"}`,
			expectError: false,
			expected:    `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := fixer.tryFixJSON(tt.jsonStr)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Check that result is valid JSON by attempting to unmarshal as both map and array
				var resultMap map[string]any
				var resultArray []any
				if json.Unmarshal([]byte(result), &resultMap) != nil && json.Unmarshal([]byte(result), &resultArray) != nil {
					t.Errorf("expected valid JSON, got %q", result)
				}
			}
		})
	}
}

func TestToolEnhanceInterceptor_Disabled(t *testing.T) {
	config := &ToolEnhanceConfig{
		Enabled:             false,
		EnsureDescriptions:  true,
		NormalizeParameters: true,
		AddMissingType:      true,
	}

	interceptor := NewToolEnhanceInterceptorWithConfig(config)

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
		Tools: []anthropic.Tool{
			{Name: "search_web", InputSchema: map[string]any{"type": "object"}},
		},
	}

	originalDesc := req.Tools[0].Description
	err := interceptor.InterceptRequest(context.Background(), req)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Tool should not be modified when disabled
	if req.Tools[0].Description != originalDesc {
		t.Error("expected tool to remain unchanged when disabled")
	}
}

func TestToolEnhanceInterceptor_MultipleTools(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
		Tools: []anthropic.Tool{
			{Name: "search_web", InputSchema: map[string]any{"type": "object"}},
			{Name: "calculate", InputSchema: map[string]any{"type": "object"}},
			{Name: "format", Description: "Format output", InputSchema: map[string]any{"type": "string"}},
		},
	}

	err := interceptor.InterceptRequest(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// First two tools should have generated descriptions
	if req.Tools[0].Description == "" {
		t.Error("expected first tool to have description")
	}
	if req.Tools[1].Description == "" {
		t.Error("expected second tool to have description")
	}

	// Third tool should keep its original description
	if req.Tools[2].Description != "Format output" {
		t.Errorf("expected original description, got %q", req.Tools[2].Description)
	}
}

func TestToolEnhanceInterceptor_ComplexSchema(t *testing.T) {
	interceptor := NewToolEnhanceInterceptor()

	complexSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"options": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required": []any{"query"},
	}

	req := &anthropic.Request{
		Model: "claude-3-5-sonnet",
		Tools: []anthropic.Tool{
			{Name: "complex_search", InputSchema: complexSchema},
		},
	}

	err := interceptor.InterceptRequest(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Schema should be properly normalized
	resultSchema, ok := req.Tools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatal("expected schema to be a map")
	}

	// Check nested properties
	props, ok := resultSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	// Check that nested structures are intact
	if options, ok := props["options"].(map[string]any); ok {
		if optType, ok := options["type"].(string); !ok || optType != "object" {
			t.Error("expected options to have type object")
		}
	}

	if tags, ok := props["tags"].(map[string]any); ok {
		if tagType, ok := tags["type"].(string); !ok || tagType != "array" {
			t.Error("expected tags to have type array")
		}
	}
}