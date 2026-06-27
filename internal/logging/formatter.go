package logging

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/sirupsen/logrus"
)

// textFormatter is a custom formatter that outputs logs in the format:
// "time [LEVEL] message"
// instead of logrus's default "time=... level=... msg=..." format.
type textFormatter struct {
	timestampFormat string
}

// newTextFormatter creates a new custom text formatter.
func newTextFormatter(timestampFormat string) *textFormatter {
	return &textFormatter{
		timestampFormat: timestampFormat,
	}
}

// Format formats a log entry.
func (f *textFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer

	// Add timestamp
	b = &bytes.Buffer{}
	b.WriteString(entry.Time.Format(f.timestampFormat))
	b.WriteString(" ")

	// Add level in brackets
	level := strings.ToUpper(entry.Level.String())
	b.WriteString("[")
	b.WriteString(level)
	b.WriteString("] ")

	// Add message
	b.WriteString(entry.Message)

	// Add fields if any
	if len(entry.Data) > 0 {
		b.WriteString(" ")
		for k, v := range entry.Data {
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", v))
			b.WriteString(" ")
		}
	}

	b.WriteString("\n")
	return b.Bytes(), nil
}

// levelToLogrusLevel converts our LogLevel to logrus Level.
func levelToLogrusLevel(level config.LogLevel) logrus.Level {
	switch level {
	case config.LevelDebug:
		return logrus.DebugLevel
	case config.LevelInfo:
		return logrus.InfoLevel
	case config.LevelWarn:
		return logrus.WarnLevel
	case config.LevelError:
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}
