package cli

import "testing"

// TestBuildBaseURL covers the URL assembly + standard-port elision rules:
// https on 443 and http on 80 produce URLs without an explicit port; every
// other port appears as :<port>.
func TestBuildBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
		host   string
		port   int
		want   string
	}{
		{"https default port omitted", "https", "api.example.com", 443, "https://api.example.com"},
		{"http default port omitted", "http", "api.example.com", 80, "http://api.example.com"},
		{"https custom port", "https", "api.example.com", 8443, "https://api.example.com:8443"},
		{"http custom port", "http", "10.0.0.5", 8081, "http://10.0.0.5:8081"},
		{"https to IP default port", "https", "10.0.0.5", 443, "https://10.0.0.5"},
		{"http to IP default port", "http", "10.0.0.5", 80, "http://10.0.0.5"},
		{"https to IPv6 default port", "https", "[::1]", 443, "https://[::1]"},
		{"https to IPv6 custom port", "https", "[::1]", 8443, "https://[::1]:8443"},
		{"http to localhost server port", "http", "localhost", 8081, "http://localhost:8081"},
		{"http to localhost default port", "http", "localhost", 80, "http://localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildBaseURL(tt.scheme, tt.host, tt.port); got != tt.want {
				t.Errorf("buildBaseURL(%q, %q, %d) = %q, want %q", tt.scheme, tt.host, tt.port, got, tt.want)
			}
		})
	}
}

func TestDefaultSchemeFor(t *testing.T) {
	tests := []struct {
		choice string
		want   string
	}{
		{"domain", "https"},
		{"ip", "http"},
		{"local", "http"},
		{"", "http"},
		{"unknown", "http"},
	}
	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			if got := defaultSchemeFor(tt.choice); got != tt.want {
				t.Errorf("defaultSchemeFor(%q) = %q, want %q", tt.choice, got, tt.want)
			}
		})
	}
}

func TestDefaultPortFor(t *testing.T) {
	tests := []struct {
		scheme string
		want   int
	}{
		{"https", 443},
		{"http", 8081},
		{"", 8081},
		{"unknown", 8081},
	}
	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			if got := defaultPortFor(tt.scheme); got != tt.want {
				t.Errorf("defaultPortFor(%q) = %d, want %d", tt.scheme, got, tt.want)
			}
		})
	}
}

func TestStripSchemePrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"api.example.com", "api.example.com"},
		{"https://api.example.com", "api.example.com"},
		{"http://api.example.com", "api.example.com"},
		{"HTTPS://api.example.com", "api.example.com"},
		{"HtTpS://api.example.com", "api.example.com"},
		{"10.0.0.5", "10.0.0.5"},
		{"", ""},
		{"http://", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := stripSchemePrefix(tt.in); got != tt.want {
				t.Errorf("stripSchemePrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
