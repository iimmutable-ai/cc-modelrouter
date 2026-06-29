package config_test

import (
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestGetAutoRestartIdle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"Empty disables", "", 0},
		{"Thirty minutes", "30m", 30 * time.Minute},
		{"Two hours", "2h", 2 * time.Hour},
		{"Complex duration", "1h30m", 90 * time.Minute},
		{"Invalid returns zero", "not-a-duration", 0},
		{"Garbage returns zero", "abc", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &config.ServerConfig{AutoRestartIdle: tt.input}
			if got := sc.GetAutoRestartIdle(); got != tt.expected {
				t.Errorf("GetAutoRestartIdle(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetAutoRestartWindow(t *testing.T) {
	oneAM := 1 * time.Hour
	fiveAM := 5 * time.Hour
	elevenPM := 23 * time.Hour
	fourAM := 4 * time.Hour

	tests := []struct {
		name        string
		input       string
		wantStart   time.Duration
		wantEnd     time.Duration
		wantEnabled bool
	}{
		{"Empty disables", "", 0, 0, false},
		{"Simple range", "01:00-05:00", oneAM, fiveAM, true},
		{"Range with spaces", "01:00 - 05:00", oneAM, fiveAM, true},
		{"Overnight wrap", "23:00-04:00", elevenPM, fourAM, true},
		{"Missing dash", "01:00", 0, 0, false},
		{"Too many dashes", "01:00-05:00-09:00", 0, 0, false},
		{"Bad start format", "1:00-05:00", 0, 0, false},
		{"Bad end format", "01:00-5:00", 0, 0, false},
		{"Garbage", "bananas", 0, 0, false},
		{"Seconds not supported", "01:00:00-05:00:00", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &config.ServerConfig{AutoRestartWindow: tt.input}
			start, end, enabled := sc.GetAutoRestartWindow()
			if enabled != tt.wantEnabled {
				t.Errorf("GetAutoRestartWindow(%q) enabled = %v, want %v", tt.input, enabled, tt.wantEnabled)
			}
			if enabled {
				if start != tt.wantStart {
					t.Errorf("GetAutoRestartWindow(%q) start = %v, want %v", tt.input, start, tt.wantStart)
				}
				if end != tt.wantEnd {
					t.Errorf("GetAutoRestartWindow(%q) end = %v, want %v", tt.input, end, tt.wantEnd)
				}
			}
		})
	}
}

func TestGetAutoRestartTimezone(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string // empty means we only check it returns non-nil Local
	}{
		{"Empty returns Local", "", ""},
		{"Valid Shanghai", "Asia/Shanghai", "Asia/Shanghai"},
		{"Valid UTC", "UTC", "UTC"},
		{"Valid America/New_York", "America/New_York", "America/New_York"},
		{"Invalid falls back to Local", "Mars/Olympus", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &config.ServerConfig{AutoRestartTimezone: tt.input}
			loc := sc.GetAutoRestartTimezone()
			if loc == nil {
				t.Fatalf("GetAutoRestartTimezone(%q) returned nil location", tt.input)
			}
			if tt.wantID != "" && loc.String() != tt.wantID {
				t.Errorf("GetAutoRestartTimezone(%q) = %q, want %q", tt.input, loc.String(), tt.wantID)
			}
		})
	}
}

func TestGetAutoRestartBackoffMax(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"Empty disables", "", 0},
		{"Ten minutes", "10m", 10 * time.Minute},
		{"Ninety seconds", "90s", 90 * time.Second},
		{"Invalid returns zero", "nope", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &config.ServerConfig{AutoRestartBackoffMax: tt.input}
			if got := sc.GetAutoRestartBackoffMax(); got != tt.expected {
				t.Errorf("GetAutoRestartBackoffMax(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
