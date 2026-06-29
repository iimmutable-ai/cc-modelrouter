// Package restartlog records auto-restart outcomes to an append-only JSONL file.
//
// The file lives at ~/.cc-modelrouter/restarts.jsonl and is the single source
// of truth for "did the restart work?". The old process writes an "initiated"
// record before syscall.Exec; the new process writes a "restarted" record on
// boot when it detects the CCRROUTER_RESTART_FROM env var. A gap between the
// two indicates a failed restart.
package restartlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Event is the kind of restart-lifecycle event.
type Event string

const (
	// EventInitiated is emitted by the old process just before syscall.Exec.
	EventInitiated Event = "initiated"
	// EventExecFailed is emitted if syscall.Exec returns an error.
	EventExecFailed Event = "exec_failed"
	// EventRestarted is emitted by the new process on boot, linked to the old
	// instance via the CCRROUTER_RESTART_FROM env var.
	EventRestarted Event = "restarted"
	// EventConfirmed is emitted after the new process serves its first request.
	// Reserved for future use; not emitted by current code.
	EventConfirmed Event = "confirmed"
)

// Record is a single restart-lifecycle entry.
type Record struct {
	TS       time.Time `json:"ts"`
	Event    Event     `json:"event"`
	From     string    `json:"from,omitempty"`
	Instance string    `json:"instance,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Idle     string    `json:"idle,omitempty"`
	Backoff  string    `json:"backoff,omitempty"`
	Error    string    `json:"error,omitempty"`
	PID      int       `json:"pid,omitempty"`
}

// Path returns the absolute path to ~/.cc-modelrouter/restarts.jsonl.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cc-modelrouter", "restarts.jsonl"), nil
}

// Append serializes r as one JSON line and appends it to the restart log.
// Best-effort: callers may ignore the error (the log must never block a restart).
func Append(r Record) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}

	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
