package config_test

import (
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		name     string
		level    config.LogLevel
		expected string
	}{
		{"Debug level", config.LevelDebug, "DEBUG"},
		{"Info level", config.LevelInfo, "INFO"},
		{"Warn level", config.LevelWarn, "WARN"},
		{"Error level", config.LevelError, "ERROR"},
		{"Unknown level", config.LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("LogLevel.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected config.LogLevel
	}{
		{"Lowercase debug", "debug", config.LevelDebug},
		{"Uppercase DEBUG", "DEBUG", config.LevelDebug},
		{"Mixed case DeBuG", "DeBuG", config.LevelDebug},
		{"Lowercase info", "info", config.LevelInfo},
		{"Uppercase INFO", "INFO", config.LevelInfo},
		{"Lowercase warn", "warn", config.LevelWarn},
		{"Uppercase WARN", "WARN", config.LevelWarn},
		{"Lowercase error", "error", config.LevelError},
		{"Uppercase ERROR", "ERROR", config.LevelError},
		{"Empty string defaults to INFO", "", config.LevelInfo},
		{"Invalid string defaults to INFO", "invalid", config.LevelInfo},
		{"Whitespace defaults to INFO", "   ", config.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.ParseLogLevel(tt.input); got != tt.expected {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLogLevelShouldLog(t *testing.T) {
	tests := []struct {
		name      string
		current   config.LogLevel
		msgLevel  config.LogLevel
		expected  bool
	}{
		{"Debug level logs debug", config.LevelDebug, config.LevelDebug, true},
		{"Debug level logs info", config.LevelDebug, config.LevelInfo, true},
		{"Debug level logs warn", config.LevelDebug, config.LevelWarn, true},
		{"Debug level logs error", config.LevelDebug, config.LevelError, true},
		{"Info level skips debug", config.LevelInfo, config.LevelDebug, false},
		{"Info level logs info", config.LevelInfo, config.LevelInfo, true},
		{"Info level logs warn", config.LevelInfo, config.LevelWarn, true},
		{"Info level logs error", config.LevelInfo, config.LevelError, true},
		{"Warn level skips debug", config.LevelWarn, config.LevelDebug, false},
		{"Warn level skips info", config.LevelWarn, config.LevelInfo, false},
		{"Warn level logs warn", config.LevelWarn, config.LevelWarn, true},
		{"Warn level logs error", config.LevelWarn, config.LevelError, true},
		{"Error level skips debug", config.LevelError, config.LevelDebug, false},
		{"Error level skips info", config.LevelError, config.LevelInfo, false},
		{"Error level skips warn", config.LevelError, config.LevelWarn, false},
		{"Error level logs error", config.LevelError, config.LevelError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.current.ShouldLog(tt.msgLevel); got != tt.expected {
				t.Errorf("LogLevel(%v).ShouldLog(%v) = %v, want %v", tt.current, tt.msgLevel, got, tt.expected)
			}
		})
	}
}
