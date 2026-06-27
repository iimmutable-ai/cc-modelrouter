// Package interceptor implements utility interceptors for cross-cutting concerns.
package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// ToolEnhanceConfig holds configuration for tool enhancement.
type ToolEnhanceConfig struct {
	// Enabled enables tool enhancement.
	Enabled bool
	// EnsureDescriptions ensures all tools have descriptions.
	EnsureDescriptions bool
	// NormalizeParameters normalizes parameter schemas.
	NormalizeParameters bool
	// AddMissingType adds missing type fields to parameters.
	AddMissingType bool
}

// DefaultToolEnhanceConfig returns a default configuration.
func DefaultToolEnhanceConfig() *ToolEnhanceConfig {
	return &ToolEnhanceConfig{
		Enabled:             true,
		EnsureDescriptions:  true,
		NormalizeParameters: true,
		AddMissingType:      true,
	}
}

// ToolEnhanceInterceptor adds/modifies tool definitions for provider compatibility.
type ToolEnhanceInterceptor struct {
	config *ToolEnhanceConfig
}

// NewToolEnhanceInterceptor creates a new ToolEnhanceInterceptor with default configuration.
func NewToolEnhanceInterceptor() *ToolEnhanceInterceptor {
	return &ToolEnhanceInterceptor{
		config: DefaultToolEnhanceConfig(),
	}
}

// NewToolEnhanceInterceptorWithConfig creates a new ToolEnhanceInterceptor with custom configuration.
func NewToolEnhanceInterceptorWithConfig(config *ToolEnhanceConfig) *ToolEnhanceInterceptor {
	return &ToolEnhanceInterceptor{
		config: config,
	}
}

// InterceptRequest enhances tool definitions for provider compatibility.
func (i *ToolEnhanceInterceptor) InterceptRequest(ctx context.Context, req *anthropic.Request) error {
	if !i.config.Enabled || len(req.Tools) == 0 {
		return nil
	}

	logging.Debugf("[ToolEnhanceInterceptor] Processing %d tool(s)", len(req.Tools))

	for idx, tool := range req.Tools {
		original := tool.Name
		modified := false

		// Ensure description exists
		if i.config.EnsureDescriptions && tool.Description == "" {
			tool.Description = i.generateDescription(tool.Name, tool.InputSchema)
			modified = true
		}

		// Normalize parameter schema
		if i.config.NormalizeParameters {
			tool.InputSchema = i.normalizeSchema(tool.InputSchema)
			modified = true
		}

		// Add missing type fields
		if i.config.AddMissingType {
			tool.InputSchema = i.addMissingTypeFields(tool.InputSchema)
			modified = true
		}

		if modified {
			logging.Debugf("[ToolEnhanceInterceptor] Enhanced tool[%d]: %s", idx, original)
			req.Tools[idx] = tool
		}
	}

	return nil
}

// generateDescription generates a description for a tool based on its name and schema.
func (i *ToolEnhanceInterceptor) generateDescription(name string, schema any) string {
	// Try to extract description from schema
	if schemaMap, ok := schema.(map[string]any); ok {
		if desc, ok := schemaMap["description"].(string); ok && desc != "" {
			return desc
		}
	}

	// Generate description from tool name
	parts := strings.Split(name, "_")
	if len(parts) == 1 {
		return fmt.Sprintf("Executes the %s tool", name)
	}

	// Convert snake_case to readable format
	var descParts []string
	for _, part := range parts {
		if part != "" {
			descParts = append(descParts, strings.Title(part))
		}
	}

	return fmt.Sprintf("%s tool for %s", strings.Join(descParts[:len(descParts)-1], " "), descParts[len(descParts)-1])
}

// normalizeSchema ensures the parameter schema follows a consistent format.
func (i *ToolEnhanceInterceptor) normalizeSchema(schema any) any {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return schema
	}

	// Make a copy to avoid modifying the original
	normalized := make(map[string]any)
	for k, v := range schemaMap {
		normalized[k] = v
	}

	// Ensure type is a string (not object/array)
	if typeVal, ok := normalized["type"]; ok {
		switch t := typeVal.(type) {
		case []any:
			// Some providers send type as array - take first element
			if len(t) > 0 {
				if firstType, ok := t[0].(string); ok {
					normalized["type"] = firstType
				}
			}
		case map[string]any:
			// Some providers send type as object - extract string value
			if typeStr, ok := t["value"].(string); ok {
				normalized["type"] = typeStr
			}
		}
	}

	// Recursively normalize properties
	if properties, ok := normalized["properties"].(map[string]any); ok {
		normalizedProps := make(map[string]any)
		for propName, propSchema := range properties {
			normalizedProps[propName] = i.normalizeSchema(propSchema)
		}
		normalized["properties"] = normalizedProps
	}

	return normalized
}

// addMissingTypeFields ensures all object properties have type fields.
func (i *ToolEnhanceInterceptor) addMissingTypeFields(schema any) any {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return schema
	}

	// Make a copy
	enhanced := make(map[string]any)
	for k, v := range schemaMap {
		enhanced[k] = v
	}

	// Check if this is an object type without explicit type
	if _, hasType := enhanced["type"]; !hasType {
		if _, hasProperties := enhanced["properties"]; hasProperties {
			enhanced["type"] = "object"
		}
	}

	// Recursively add type to properties
	if properties, ok := enhanced["properties"].(map[string]any); ok {
		enhancedProps := make(map[string]any)
		for propName, propSchema := range properties {
			enhancedProps[propName] = i.addMissingTypeFields(propSchema)
		}
		enhanced["properties"] = enhancedProps
	}

	// Handle items in array type
	if items, ok := enhanced["items"]; ok {
		enhanced["items"] = i.addMissingTypeFields(items)
	}

	return enhanced
}

// ValidateToolSchema validates a tool schema for common issues.
func (i *ToolEnhanceInterceptor) ValidateToolSchema(schema any) error {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("schema must be an object")
	}

	// Check for required type field
	if schemaType, ok := schemaMap["type"]; !ok {
		return fmt.Errorf("schema missing required 'type' field")
	} else if typeStr, ok := schemaType.(string); !ok {
		return fmt.Errorf("'type' field must be a string, got %T", schemaType)
	} else if typeStr != "object" {
		// Non-object types are simpler to validate
		return nil
	}

	// For object type, check properties
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return nil // Properties are optional
	}

	// Validate each property
	for propName, propSchema := range properties {
		if err := i.validateProperty(propName, propSchema); err != nil {
			return err
		}
	}

	return nil
}

// validateProperty validates a single property schema.
func (i *ToolEnhanceInterceptor) validateProperty(name string, schema any) error {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("property '%s' schema must be an object", name)
	}

	// Check type
	schemaType, ok := schemaMap["type"]
	if !ok {
		return fmt.Errorf("property '%s' missing 'type' field", name)
	}

	typeStr, ok := schemaType.(string)
	if !ok {
		return fmt.Errorf("property '%s' 'type' must be a string", name)
	}

	// Handle array type with items
	if typeStr == "array" {
		items, ok := schemaMap["items"]
		if !ok {
			return fmt.Errorf("property '%s' array must have 'items'", name)
		}
		return i.validateProperty(name+".items", items)
	}

	// Handle object type with properties
	if typeStr == "object" {
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			for propName, propSchema := range props {
				if err := i.validateProperty(name+"."+propName, propSchema); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// FixToolSchema attempts to fix common issues in a tool schema.
func (i *ToolEnhanceInterceptor) FixToolSchema(schema any) (any, error) {
	// First validate
	if err := i.ValidateToolSchema(schema); err == nil {
		return schema, nil // Already valid
	}

	// Try to fix the schema
	fixed := i.addMissingTypeFields(schema)
	fixed = i.normalizeSchema(fixed)

	// Validate again
	if err := i.ValidateToolSchema(fixed); err != nil {
		return nil, fmt.Errorf("unable to fix schema: %w", err)
	}

	return fixed, nil
}

// MergeToolSchemas merges multiple tool schemas into one.
// Useful for combining tools from different providers.
func (i *ToolEnhanceInterceptor) MergeToolSchemas(schemas []any) (any, error) {
	if len(schemas) == 0 {
		return nil, fmt.Errorf("no schemas to merge")
	}

	if len(schemas) == 1 {
		return schemas[0], nil
	}

	// Create merged schema
	merged := map[string]any{
		"type":       "object",
		"properties": make(map[string]any),
	}

	var required []string

	for _, schema := range schemas {
		schemaMap, ok := schema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema must be an object")
		}

		// Merge properties
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			for k, v := range props {
				merged["properties"].(map[string]any)[k] = v
			}
		}

		// Merge required fields
		if reqFields, ok := schemaMap["required"].([]any); ok {
			for _, field := range reqFields {
				if fieldStr, ok := field.(string); ok {
					required = append(required, fieldStr)
				}
			}
		}
	}

	if len(required) > 0 {
		merged["required"] = required
	}

	return merged, nil
}

// ToolSchemaFixer is a helper to fix JSON marshaling issues in tool arguments.
func (i *ToolEnhanceInterceptor) ToolSchemaFixer() ToolSchemaFixer {
	return ToolSchemaFixer{interceptor: i}
}

// ToolSchemaFixer provides methods to fix JSON-related issues in tool schemas.
type ToolSchemaFixer struct {
	interceptor *ToolEnhanceInterceptor
}

// FixToolCallJSON fixes JSON marshaling issues in tool call arguments.
func (f ToolSchemaFixer) FixToolCallJSON(rawArgs json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal(rawArgs, &result); err != nil {
		// Try to fix common JSON issues
		fixed, fixErr := f.tryFixJSON(string(rawArgs))
		if fixErr != nil {
			return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
		if err := json.Unmarshal([]byte(fixed), &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal fixed tool arguments: %w", err)
		}
	}
	return result, nil
}

// tryFixJSON attempts to fix common JSON syntax issues.
func (f ToolSchemaFixer) tryFixJSON(jsonStr string) (string, error) {
	// Remove trailing commas
	jsonStr = strings.ReplaceAll(jsonStr, ",\n", "\n")
	jsonStr = strings.ReplaceAll(jsonStr, ",}", "}")
	jsonStr = strings.ReplaceAll(jsonStr, ",]", "]")

	// Fix unquoted keys (simple heuristic)
	jsonStr = strings.ReplaceAll(jsonStr, " {", " {")
	jsonStr = strings.ReplaceAll(jsonStr, "{ ", "{")
	jsonStr = strings.ReplaceAll(jsonStr, " :", ":")

	return jsonStr, nil
}