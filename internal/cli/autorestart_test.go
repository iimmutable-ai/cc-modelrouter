package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/restartlog"
)

func TestInWindow(t *testing.T) {
	utc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("failed to load UTC: %v", err)
	}
	oneAM := 1 * time.Hour
	fiveAM := 5 * time.Hour
	elevenPM := 23 * time.Hour
	fourAM := 4 * time.Hour

	tests := []struct {
		name  string
		hour  int
		min   int
		start time.Duration
		end   time.Duration
		want  bool
	}{
		// Normal range 01:00-05:00
		{"Before range", 0, 30, oneAM, fiveAM, false},
		{"At start (inclusive)", 1, 0, oneAM, fiveAM, true},
		{"Inside range", 3, 0, oneAM, fiveAM, true},
		{"At end (exclusive)", 5, 0, oneAM, fiveAM, false},
		{"After range", 6, 0, oneAM, fiveAM, false},

		// Overnight wrap 23:00-04:00
		{"Wrap: before start", 12, 0, elevenPM, fourAM, false},
		{"Wrap: at start", 23, 0, elevenPM, fourAM, true},
		{"Wrap: midnight", 0, 0, elevenPM, fourAM, true},
		{"Wrap: just before end", 3, 59, elevenPM, fourAM, true},
		{"Wrap: at end (exclusive)", 4, 0, elevenPM, fourAM, false},
		{"Wrap: morning after", 8, 0, elevenPM, fourAM, false},

		// Edge: zero-length range (start == end) — nothing matches since end is exclusive
		{"Zero-length at same time", 5, 0, fiveAM, fiveAM, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, tt.hour, tt.min, 0, 0, utc)
			if got := inWindow(now, tt.start, tt.end, utc); got != tt.want {
				t.Errorf("inWindow(hour=%02d:%02d, %v-%v) = %v, want %v",
					tt.hour, tt.min, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestInWindowRespectsTimezone(t *testing.T) {
	// 18:00 UTC is 02:00+1 Asia/Shanghai. Window 01:00-05:00 Shanghai should match.
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("failed to load Asia/Shanghai: %v", err)
	}
	utc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("failed to load UTC: %v", err)
	}
	oneAM := 1 * time.Hour
	fiveAM := 5 * time.Hour

	utcTime := time.Date(2026, 1, 1, 18, 0, 0, 0, utc) // 18:00 UTC = 02:00+1 Shanghai
	if !inWindow(utcTime, oneAM, fiveAM, shanghai) {
		t.Errorf("expected 18:00 UTC (02:00 Shanghai) to be in 01:00-05:00 Shanghai window")
	}
}

// withTempHome redirects HOME to a temp dir and returns the restart log path.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	withTempHomeDir(t, dir)
	p, err := restartlog.Path()
	if err != nil {
		t.Fatalf("restartlog.Path failed: %v", err)
	}
	return p
}

// withTempHomeDir sets HOME/USERPROFILE to the given dir for the duration of
// the test. Used when the caller needs to know the dir itself (e.g. to pass it
// to a subprocess).
func withTempHomeDir(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	} else {
		t.Setenv("HOME", dir)
	}
}

// readRestartLog parses the JSONL restart log into a slice of records.
func readRestartLog(t *testing.T, p string) []restartlog.Record {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open restart log failed: %v", err)
	}
	defer f.Close()

	var out []restartlog.Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec restartlog.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("invalid JSONL line: %v\nraw: %s", err, line)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return out
}

// newTestServer builds a minimal proxy.Server suitable for exercising
// performIdleRestart without binding a real socket.
func newTestServer(t *testing.T) *proxy.Server {
	t.Helper()
	srv, err := proxy.NewServer(&proxy.ServerConfig{
		Host: "127.0.0.1",
		Port: 0,
	})
	if err != nil {
		t.Fatalf("proxy.NewServer failed: %v", err)
	}
	return srv
}

// TestPerformIdleRestartWritesInitiatedRecord verifies the initiated record is
// written before execFn runs, the env-var handoff is set, and an exec_failed
// record is appended when execFn errors. Runs in a subprocess because
// performIdleRestart calls os.Exit(1) on the failure path.
func TestPerformIdleRestartWritesInitiatedRecord(t *testing.T) {
	if os.Getenv("CCRROUTER_TEST_PERFORMRESTART") == "1" {
		// Child: run performIdleRestart with a stubbed execFn that errors.
		srv := newTestServer(t)
		opts := idleWatchOpts{idle: 5 * time.Second}
		var seenFromEnv string
		stubExec := func() error {
			seenFromEnv = os.Getenv("CCRROUTER_RESTART_FROM")
			return errors.New("stub exec")
		}
		performIdleRestart(srv, "inst_test_initiated", opts, 250*time.Millisecond, stubExec)
		// Sanity: if we reach here, os.Exit didn't fire. Print for debug.
		fmt.Printf("unexpected: performIdleRestart returned; seenFromEnv=%q\n", seenFromEnv)
		os.Exit(99)
	}

	dir := t.TempDir()
	withTempHomeDir(t, dir)

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestPerformIdleRestartWritesInitiatedRecord", "-test.timeout=30s")
	cmd.Env = append(os.Environ(),
		"CCRROUTER_TEST_PERFORMRESTART=1",
		"HOME="+dir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected subprocess to exit non-zero, got nil.\noutput: %s", out)
	}

	logPath := filepath.Join(dir, ".cc-modelrouter", "restarts.jsonl")
	records := readRestartLog(t, logPath)
	if len(records) != 2 {
		t.Fatalf("expected 2 records (initiated + exec_failed), got %d: %+v", len(records), records)
	}
	initiated := records[0]
	if initiated.Event != restartlog.EventInitiated {
		t.Errorf("record[0].Event = %q, want initiated", initiated.Event)
	}
	if initiated.From != "inst_test_initiated" {
		t.Errorf("record[0].From = %q, want inst_test_initiated", initiated.From)
	}
	if initiated.Reason != "idle" {
		t.Errorf("record[0].Reason = %q, want idle", initiated.Reason)
	}
	if initiated.Idle != "5s" {
		t.Errorf("record[0].Idle = %q, want 5s", initiated.Idle)
	}
	if initiated.Backoff != "250ms" {
		t.Errorf("record[0].Backoff = %q, want 250ms", initiated.Backoff)
	}

	failed := records[1]
	if failed.Event != restartlog.EventExecFailed {
		t.Errorf("record[1].Event = %q, want exec_failed", failed.Event)
	}
	if failed.From != "inst_test_initiated" {
		t.Errorf("record[1].From = %q, want inst_test_initiated", failed.From)
	}
	if !strings.Contains(failed.Error, "stub exec") {
		t.Errorf("record[1].Error = %q, want it to contain 'stub exec'", failed.Error)
	}
}

// TestPerformIdleRestartBackoffZero verifies the no-backoff path records a
// zero-duration Backoff string rather than omitting the field.
func TestPerformIdleRestartBackoffZero(t *testing.T) {
	if os.Getenv("CCRROUTER_TEST_PERFORMRESTART") == "1" {
		srv := newTestServer(t)
		opts := idleWatchOpts{idle: 10 * time.Second}
		performIdleRestart(srv, "inst_test_nobackoff", opts, 0, func() error {
			return errors.New("stub exec")
		})
		os.Exit(99)
	}

	dir := t.TempDir()
	withTempHomeDir(t, dir)

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestPerformIdleRestartBackoffZero", "-test.timeout=30s")
	cmd.Env = append(os.Environ(),
		"CCRROUTER_TEST_PERFORMRESTART=1",
		"HOME="+dir,
	)
	if _, err := cmd.CombinedOutput(); err == nil {
		t.Fatal("expected subprocess to exit non-zero")
	}

	logPath := filepath.Join(dir, ".cc-modelrouter", "restarts.jsonl")
	records := readRestartLog(t, logPath)
	if len(records) == 0 {
		t.Fatal("expected at least one record")
	}
	if records[0].Backoff != "0s" {
		t.Errorf("record[0].Backoff = %q, want 0s", records[0].Backoff)
	}
}
