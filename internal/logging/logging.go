// Package logging handles setup and configuration of application logging.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/sirupsen/logrus"
)

var (
	// logger is the global logrus instance
	logger *logrus.Logger
	// currentLevel holds the current log level (atomic for thread safety)
	currentLevel atomic.Int64
)

// SyncingFileWriter wraps an *os.File and syncs to disk after each write.
// This ensures log messages are immediately written to disk and not buffered.
type SyncingFileWriter struct {
	*os.File
}

// Write writes data to the file and immediately syncs to disk.
// This prevents log buffering issues where logs might not appear in the file
// until the buffer is flushed or the program exits.
func (s *SyncingFileWriter) Write(p []byte) (n int, err error) {
	n, err = s.File.Write(p)
	if err != nil {
		return n, err
	}
	// Force sync to disk after each write to ensure logs are immediately persisted.
	// Ignore sync errors for non-seekable files like stdout/stderr.
	_ = s.File.Sync()
	return n, nil
}

// Close closes the underlying file.
func (s *SyncingFileWriter) Close() error {
	return s.File.Close()
}

func init() {
	// Initialize logger with defaults on package load
	logger = logrus.New()
	logger.SetOutput(io.Discard)
	// Use custom formatter for original format: "time [LEVEL] message"
	logger.SetFormatter(newTextFormatter("2006/01/02 15:04:05"))
	// Set default level to info (will be overridden by Init)
	currentLevel.Store(int64(config.LevelInfo))
}

// Init initializes logging based on the provided configuration.
// It redirects the logrus output to the configured destination.
// Logging output is controlled by the Destination configuration.
//
// The function returns an error if the log destination cannot be initialized,
// or a cleanup function that should be called on shutdown to close any open files.
func Init(cfg *config.LoggingConfig) (func(), error) {
	// Set the log level atomically
	level := int64(config.LevelInfo) // default
	if cfg != nil && cfg.IsEnabled() {
		level = int64(cfg.GetLevel())
	}
	currentLevel.Store(level)

	// Update logrus level
	updateLogLevel(config.LogLevel(level))

	// If config is nil or logging is not explicitly enabled, disable logging
	if cfg == nil || !cfg.IsEnabled() {
		logger.SetOutput(io.Discard)
		return func() {}, nil
	}

	// Get the primary log writer
	writer, err := cfg.GetLogWriter()
	if err != nil {
		return nil, fmt.Errorf("failed to create log writer: %w", err)
	}

	// Wrap file writer with syncing wrapper to ensure logs are immediately flushed to disk
	// This prevents log buffering issues where logs might not appear in the file
	if file, ok := writer.(*os.File); ok {
		writer = &SyncingFileWriter{File: file}
	}

	// Configure the logger output
	logger.SetOutput(writer)

	// Return a cleanup function
	cleanup := func() {
		if closer, ok := writer.(io.Closer); ok && cfg.ShouldLogToFile() {
			closer.Close()
		}
		// Reset to discard on cleanup
		logger.SetOutput(io.Discard)
	}

	return cleanup, nil
}

// InitWithPath initializes logging with a specific file path.
// This is a convenience function for programmatic setup.
func InitWithPath(logPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Wrap with syncing writer to ensure logs are immediately flushed to disk
	writer := &SyncingFileWriter{File: f}
	logger.SetOutput(writer)

	return func() {
		f.Close()
		logger.SetOutput(io.Discard)
	}, nil
}

// InitToStdout redirects logs to stdout.
func InitToStdout() {
	logger.SetOutput(os.Stdout)
}

// InitToStderr redirects logs to stderr.
func InitToStderr() {
	logger.SetOutput(os.Stderr)
}

// InitToSilent discards all log output.
func InitToSilent() {
	logger.SetOutput(io.Discard)
}

// DefaultLogPath returns the default log file path.
// Note: For per-instance logging, use GetLogPathWithInstance instead.
func DefaultLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cc-modelrouter", "router.log"), nil
}

// LogsDir returns the logs directory path.
func LogsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cc-modelrouter", "logs"), nil
}

// GetLogger returns the underlying logrus logger for advanced usage.
func GetLogger() *logrus.Logger {
	return logger
}

// GetWriter returns the current log writer.
// This is provided for compatibility with http.Server.ErrorLog.
// Returns io.Discard if logging has not been initialized.
func GetWriter() io.Writer {
	return logger.Out
}

// updateLogLevel updates the logrus level based on our config level.
func updateLogLevel(level config.LogLevel) {
	logger.SetLevel(levelToLogrusLevel(level))
}

// shouldLog returns true if a message at the given level should be logged.
func shouldLog(level config.LogLevel) bool {
	current := config.LogLevel(currentLevel.Load())
	return current.ShouldLog(level)
}

// Infof logs an info message.
func Infof(format string, v ...interface{}) {
	if !shouldLog(config.LevelInfo) {
		return
	}
	logger.Infof(format, v...)
}

// Warnf logs a warning message.
func Warnf(format string, v ...interface{}) {
	if !shouldLog(config.LevelWarn) {
		return
	}
	logger.Warnf(format, v...)
}

// Errorf logs an error message.
func Errorf(format string, v ...interface{}) {
	if !shouldLog(config.LevelError) {
		return
	}
	logger.Errorf(format, v...)
}

// Debugf logs a debug message.
func Debugf(format string, v ...interface{}) {
	if !shouldLog(config.LevelDebug) {
		return
	}
	logger.Debugf(format, v...)
}

// Streamf logs a streaming-related message (INFO level by default).
// These are verbose per-event logs that should be filtered at DEBUG level.
func Streamf(format string, v ...interface{}) {
	if !shouldLog(config.LevelInfo) {
		return
	}
	logger.Infof("[STREAM] "+format, v...)
}

// StreamDebugf logs detailed streaming debug information (DEBUG level).
func StreamDebugf(format string, v ...interface{}) {
	if !shouldLog(config.LevelDebug) {
		return
	}
	logger.Debugf("[STREAM-DEBUG] "+format, v...)
}
