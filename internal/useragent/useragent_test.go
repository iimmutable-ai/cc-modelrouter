package useragent

import (
	"strings"
	"testing"
)

func TestDefault_LooksLikeAnthropicSDK(t *testing.T) {
	got := Default()
	if !strings.HasPrefix(got, "@anthropic-ai/sdk/") {
		t.Errorf("Default() = %q, want prefix @anthropic-ai/sdk/", got)
	}
	if !strings.Contains(got, "bun/") && !strings.Contains(got, "node/") {
		t.Errorf("Default() = %q, want runtime segment (bun/ or node/)", got)
	}
}

func TestResolve(t *testing.T) {
	def := Default()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to default", "", def},
		{"whitespace falls back to default", "   \t\n  ", def},
		{"tabs-only falls back to default", "\t\t", def},
		{"custom value passes through", "MyCustom/9.9", "MyCustom/9.9"},
		{"custom value with surrounding spaces is trimmed", "  MyCustom/9.9  ", "MyCustom/9.9"},
		{"preserves internal spaces", "My Agent/1.0 (comment)", "My Agent/1.0 (comment)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.in); got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
