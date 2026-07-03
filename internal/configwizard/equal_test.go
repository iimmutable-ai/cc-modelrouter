package configwizard

import (
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestTLSConfigsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *config.TLSConfig
		b    *config.TLSConfig
		want bool
	}{
		{"both nil", nil, nil, true},
		{"a nil", nil, &config.TLSConfig{}, false},
		{"b nil", &config.TLSConfig{}, nil, false},
		{"both empty", &config.TLSConfig{}, &config.TLSConfig{}, true},
		{
			"equal manual",
			&config.TLSConfig{CertFile: "/c.pem", KeyFile: "/k.pem", Redirect: true},
			&config.TLSConfig{CertFile: "/c.pem", KeyFile: "/k.pem", Redirect: true},
			true,
		},
		{
			"equal autocert",
			&config.TLSConfig{Domain: "api.example.com", Redirect: true},
			&config.TLSConfig{Domain: "api.example.com", Redirect: true},
			true,
		},
		{"certFile differs", &config.TLSConfig{CertFile: "/a"}, &config.TLSConfig{CertFile: "/b"}, false},
		{"keyFile differs", &config.TLSConfig{KeyFile: "/a"}, &config.TLSConfig{KeyFile: "/b"}, false},
		{"domain differs", &config.TLSConfig{Domain: "a.com"}, &config.TLSConfig{Domain: "b.com"}, false},
		{"redirect differs", &config.TLSConfig{Redirect: false}, &config.TLSConfig{Redirect: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tlsConfigsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("tlsConfigsEqual(%+v, %+v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestConfigsEqual_ServerScreenFields(t *testing.T) {
	// Baseline config used as the "previous" state for every mutation case.
	// Field values are chosen so that mutation is detectable (no zero-value false negatives).
	base := func() *config.Config {
		return &config.Config{
			Server: config.ServerConfig{
				Host:                  "localhost",
				Port:                  8081,
				AutoRestartIdle:       "30m",
				AutoRestartWindow:     "03:00-05:00",
				AutoRestartTimezone:   "Asia/Macau",
				AutoRestartBackoffMax: "5m",
				TLS: &config.TLSConfig{
					CertFile: "/etc/certs/fullchain.pem",
					KeyFile:  "/etc/certs/privkey.pem",
					Redirect: true,
				},
			},
			Router: config.RouterConfig{
				MaxRetries: 3,
				RetryDelay: "500ms",
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantNot bool // true = expect configsEqual to return false after mutation
	}{
		{"host differs", func(c *config.Config) { c.Server.Host = "0.0.0.0" }, true},
		{"port differs", func(c *config.Config) { c.Server.Port = 9090 }, true},
		{"maxRetries differs", func(c *config.Config) { c.Router.MaxRetries = 99 }, true},
		{"retryDelay differs", func(c *config.Config) { c.Router.RetryDelay = "1s" }, true},
		{"autoRestartIdle differs", func(c *config.Config) { c.Server.AutoRestartIdle = "1h" }, true},
		{"autoRestartWindow differs", func(c *config.Config) { c.Server.AutoRestartWindow = "04:00-06:00" }, true},
		{"autoRestartTimezone differs", func(c *config.Config) { c.Server.AutoRestartTimezone = "UTC" }, true},
		{"autoRestartBackoffMax differs", func(c *config.Config) { c.Server.AutoRestartBackoffMax = "30m" }, true},
		{"TLS certFile differs", func(c *config.Config) { c.Server.TLS.CertFile = "/other.pem" }, true},
		{"TLS redirect differs", func(c *config.Config) { c.Server.TLS.Redirect = false }, true},
		{"TLS cleared (nil)", func(c *config.Config) { c.Server.TLS = nil }, true},
		{"no changes (identity)", func(c *config.Config) {}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := base()
			b := base()
			tt.mutate(b)
			got := configsEqual(a, b)
			if tt.wantNot && got {
				t.Errorf("configsEqual(base, mutated) = true, want false (mutation: %s)", tt.name)
			}
			if !tt.wantNot && !got {
				t.Errorf("configsEqual(base, base) = false, want true (case: %s)", tt.name)
			}
		})
	}
}

func TestConfigsEqual_TLSNilOnBoth(t *testing.T) {
	a := &config.Config{}
	b := &config.Config{}
	if !configsEqual(a, b) {
		t.Errorf("configsEqual with TLS=nil on both = false, want true")
	}
}
