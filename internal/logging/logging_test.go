// Package logging tests
package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestInitDefaultConfig(t *testing.T) {
	cfg := &config.LoggingConfig{
		Enabled:     true,
		Destination: "file",
	}

	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	Infof("test message")

	if cleanup != nil {
		cleanup()
	}
}

func TestInitStdout(t *testing.T) {
	cfg := &config.LoggingConfig{
		Enabled:     true,
		Destination: "stdout",
	}

	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer cleanup()

	Infof("test to stdout")
}

func TestInitStderr(t *testing.T) {
	cfg := &config.LoggingConfig{
		Enabled:     true,
		Destination: "stderr",
	}

	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer cleanup()

	Infof("test to stderr")
}

func TestInitWithFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	cfg := &config.LoggingConfig{
		Enabled:     true,
		Destination: "file",
		FilePath:    logPath,
	}

	cleanup, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Use the logging package's Infof, not standard library's log.Println
	Infof("test message to file")

	if cleanup != nil {
		cleanup()
	}

	// Verify the file was created and contains the message
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}
}

func TestInitWithPath(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-path.log")

	cleanup, err := InitWithPath(logPath)
	if err != nil {
		t.Fatalf("InitWithPath failed: %v", err)
	}

	// Use the logging package's Infof, not standard library's log.Println
	Infof("test message")

	if cleanup != nil {
		cleanup()
	}

	// Verify the file was created
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}
}

func TestDefaultLogPath(t *testing.T) {
	path, err := DefaultLogPath()
	if err != nil {
		t.Fatalf("DefaultLogPath failed: %v", err)
	}

	if filepath.Base(path) != "router.log" {
		t.Errorf("Expected router.log, got %s", filepath.Base(path))
	}
}

func TestHelperFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func()
	}{
		{"Infof", func() { Infof("test info") }},
		{"Warnf", func() { Warnf("test warn") }},
		{"Errorf", func() { Errorf("test error") }},
		{"Debugf", func() { Debugf("test debug") }},
		{"InitToStdout", func() { InitToStdout() }},
		{"InitToStderr", func() { InitToStderr() }},
		{"InitToSilent", func() { InitToSilent() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just ensure these don't panic
			defer func() {
				InitToStderr() // Reset
			}()
			tt.fn()
		})
	}
}

func TestLoggingConfigMethods(t *testing.T) {
	tests := []struct {
		name           string
		cfg            config.LoggingConfig
		wantEnabled    bool
		wantToFile     bool
		wantToConsole  bool
		wantLogPathErr bool
	}{
		{
			name: "file destination",
			cfg: config.LoggingConfig{
				Enabled:     true,
				Destination: "file",
			},
			wantEnabled:   true,
			wantToFile:    true,
			wantToConsole: false,
		},
		{
			name: "stdout destination",
			cfg: config.LoggingConfig{
				Enabled:     true,
				Destination: "stdout",
			},
			wantEnabled:   true,
			wantToFile:    false,
			wantToConsole: true,
		},
		{
			name: "stderr destination",
			cfg: config.LoggingConfig{
				Enabled:     true,
				Destination: "stderr",
			},
			wantEnabled:   true,
			wantToFile:    false,
			wantToConsole: true,
		},
		{
			name: "custom file path",
			cfg: config.LoggingConfig{
				Enabled:     true,
				Destination: "/tmp/test.log",
			},
			wantEnabled:   true,
			wantToFile:    true,
			wantToConsole: false,
		},
		{
			name: "disabled logging",
			cfg: config.LoggingConfig{
				Enabled:     false,
				Destination: "file",
			},
			wantEnabled:   false,
			wantToFile:    true,
			wantToConsole: false,
		},
		{
			name: "default (not enabled)",
			cfg: config.LoggingConfig{
				Destination: "file",
			},
			wantEnabled:   false,
			wantToFile:    true,
			wantToConsole: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEnabled(); got != tt.wantEnabled {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.wantEnabled)
			}
			if got := tt.cfg.ShouldLogToFile(); got != tt.wantToFile {
				t.Errorf("ShouldLogToFile() = %v, want %v", got, tt.wantToFile)
			}
			if got := tt.cfg.ShouldLogToConsole(); got != tt.wantToConsole {
				t.Errorf("ShouldLogToConsole() = %v, want %v", got, tt.wantToConsole)
			}
		})
	}
}

func TestInitWithLevel(t *testing.T) {
	tests := []struct {
		name           string
		level          string
		shouldLogDebug bool
		shouldLogInfo  bool
		shouldLogWarn  bool
		shouldLogError bool
	}{
		{
			name:           "debug level logs everything",
			level:          "debug",
			shouldLogDebug: true,
			shouldLogInfo:  true,
			shouldLogWarn:  true,
			shouldLogError: true,
		},
		{
			name:           "info level excludes debug",
			level:          "info",
			shouldLogDebug: false,
			shouldLogInfo:  true,
			shouldLogWarn:  true,
			shouldLogError: true,
		},
		{
			name:           "warn level only logs warn and error",
			level:          "warn",
			shouldLogDebug: false,
			shouldLogInfo:  false,
			shouldLogWarn:  true,
			shouldLogError: true,
		},
		{
			name:           "error level only logs errors",
			level:          "error",
			shouldLogDebug: false,
			shouldLogInfo:  false,
			shouldLogWarn:  false,
			shouldLogError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Save original logger output
			originalLogger := GetLogger()
			originalOutput := originalLogger.Out
			defer originalLogger.SetOutput(originalOutput)

			// Set the log level by calling Init with a minimal config
			// Init handles both currentLevel and logrus level
			cfg := &config.LoggingConfig{
				Enabled:     true,
				Destination: "stdout",
				Level:       tt.level,
			}
			cleanup, err := Init(cfg)
			if err != nil {
				t.Fatalf("Init failed: %v", err)
			}
			defer cleanup()

			// Set logger output to buffer AFTER Init, so it overrides the destination
			originalLogger.SetOutput(&buf)

			// Log at all levels
			Debugf("debug message")
			Infof("info message")
			Warnf("warn message")
			Errorf("error message")

			output := buf.String()

			// Check each level
			if tt.shouldLogDebug && !strings.Contains(output, "[DEBUG]") {
				t.Error("Expected debug message in output")
			}
			if !tt.shouldLogDebug && strings.Contains(output, "[DEBUG]") {
				t.Error("Did not expect debug message in output")
			}

			if tt.shouldLogInfo && !strings.Contains(output, "[INFO]") {
				t.Error("Expected info message in output")
			}
			if !tt.shouldLogInfo && strings.Contains(output, "[INFO]") {
				t.Error("Did not expect info message in output")
			}

			if tt.shouldLogWarn && !strings.Contains(output, "[WARNING]") {
				t.Error("Expected warn message in output")
			}
			if !tt.shouldLogWarn && strings.Contains(output, "[WARNING]") {
				t.Error("Did not expect warn message in output")
			}

			if tt.shouldLogError && !strings.Contains(output, "[ERROR]") {
				t.Error("Expected error message in output")
			}
			if !tt.shouldLogError && strings.Contains(output, "[ERROR]") {
				t.Error("Did not expect error message in output")
			}
		})
	}
}
