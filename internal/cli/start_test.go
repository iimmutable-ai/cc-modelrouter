package cli

import (
	"testing"
	"time"
)

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"api.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := isLocalhost(tt.host)
			if got != tt.want {
				t.Errorf("isLocalhost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestRemainingOrInterval(t *testing.T) {
	tests := []struct {
		name      string
		idle      time.Duration
		lastAct   time.Duration // ago (negative = in the past)
		interval  time.Duration
		wantMin   time.Duration // >= this
		wantExact bool
		wantExactVal time.Duration
	}{
		{
			name:    "remaining greater than interval",
			idle:    30 * time.Minute,
			lastAct: 5 * time.Minute,
			interval: 1 * time.Minute,
			wantMin: 24 * time.Minute, // accounts for time.Since drift
		},
		{
			name:     "remaining less than interval",
			idle:     5 * time.Minute,
			lastAct:  4 * time.Minute,
			interval: 2 * time.Minute,
			wantMin:  2 * time.Minute,
		},
		{
			name:         "remaining exactly interval",
			idle:         10 * time.Minute,
			lastAct:      8 * time.Minute,
			interval:     2 * time.Minute,
			wantMin:      2 * time.Minute,
			wantExact:    true,
			wantExactVal: 2 * time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastActivity := time.Now().Add(-tt.lastAct)
			got := remainingOrInterval(tt.idle, lastActivity, tt.interval)
			if got < tt.wantMin {
				t.Errorf("remainingOrInterval = %v, want >= %v", got, tt.wantMin)
			}
			if tt.wantExact && got != tt.wantExactVal {
				t.Errorf("remainingOrInterval = %v, want %v", got, tt.wantExactVal)
			}
		})
	}
}

func TestInWindow_Start(t *testing.T) {
	tz := time.FixedZone("UTC+8", 8*3600)

	tests := []struct {
		name  string
		hour  int
		min   int
		start time.Duration
		end   time.Duration
		want  bool
	}{
		{"within window 9-17 at 10:00", 10, 0, 9 * time.Hour, 17 * time.Hour, true},
		{"within window 9-17 at 9:00 exactly", 9, 0, 9 * time.Hour, 17 * time.Hour, true},
		{"before window 9-17 at 8:59", 8, 59, 9 * time.Hour, 17 * time.Hour, false},
		{"after window 9-17 at 17:00", 17, 0, 9 * time.Hour, 17 * time.Hour, false},
		{"within window 9-17 at 16:59", 16, 59, 9 * time.Hour, 17 * time.Hour, true},
		{"overnight wrap 23-5 at 0:00", 0, 0, 23 * time.Hour, 5 * time.Hour, true},
		{"overnight wrap 23-5 at 23:00", 23, 0, 23 * time.Hour, 5 * time.Hour, true},
		{"overnight wrap 23-5 at 4:59", 4, 59, 23 * time.Hour, 5 * time.Hour, true},
		{"overnight wrap 23-5 at 5:00", 5, 0, 23 * time.Hour, 5 * time.Hour, false},
		{"overnight wrap 23-5 at 22:00", 22, 0, 23 * time.Hour, 5 * time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 7, 7, tt.hour, tt.min, 0, 0, tz)
			got := inWindow(now, tt.start, tt.end, tz)
			if got != tt.want {
				t.Errorf("inWindow(%02d:%02d, %v-%v) = %v, want %v",
					tt.hour, tt.min, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestNewStartCommand(t *testing.T) {
	cmd := NewStartCommand()

	if cmd.Use != "start" {
		t.Errorf("expected Use %q, got %q", "start", cmd.Use)
	}

	expectedFlags := []string{
		"config", "port", "host", "log-destination", "log-level", "profile",
		"auto-restart-idle", "tls-cert", "tls-key", "tls-domain", "tls-redirect",
	}
	for _, f := range expectedFlags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag", f)
		}
	}
}

func TestNewStopCommand(t *testing.T) {
	cmd := NewStopCommand()

	if cmd.Use != "stop [instance-id]" {
		t.Errorf("expected Use %q, got %q", "stop [instance-id]", cmd.Use)
	}

	f := cmd.Flags().Lookup("force")
	if f == nil {
		t.Fatal("expected --force flag")
	}
	if f.DefValue != "false" {
		t.Errorf("expected --force default %q, got %q", "false", f.DefValue)
	}
}
