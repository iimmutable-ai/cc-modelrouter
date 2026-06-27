package cli

import "testing"

// TestIsLocalHostURL covers the loopback classifier that gates the gen settings
// non-local deployment warning. White-box (package cli) because it asserts on
// the unexported isLocalHostURL helper; per CLAUDE.md test-organization rules
// white-box tests live alongside source rather than under test/.
func TestIsLocalHostURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"localhost hostname", "http://localhost:8081", true},
		{"ipv4 loopback", "http://127.0.0.1:8081", true},
		{"ipv6 loopback", "http://[::1]:8081", true},
		{"public ipv4", "http://43.108.32.178:8081", false},
		{"remote hostname", "http://myserver:8081", false},
		{"public domain", "https://router.example.com:8081", false},
		{"localhost no port", "http://localhost", true},
		{"unparseable url", "://not-a-url", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalHostURL(tt.url); got != tt.want {
				t.Errorf("isLocalHostURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
