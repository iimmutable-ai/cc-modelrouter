package restartlog_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/restartlog"
)

// withTempHome redirects HOME to a temp dir for the duration of the test and
// returns the path to the restarts.jsonl that Path() will resolve to.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	} else {
		t.Setenv("HOME", dir)
	}

	p, err := restartlog.Path()
	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	return p
}

func TestPathUnderHome(t *testing.T) {
	p := withTempHome(t)
	if filepath.Base(p) != "restarts.jsonl" {
		t.Errorf("Path base = %q, want restarts.jsonl", filepath.Base(p))
	}
	if filepath.Base(filepath.Dir(p)) != ".cc-modelrouter" {
		t.Errorf("Path parent = %q, want .cc-modelrouter", filepath.Base(filepath.Dir(p)))
	}
}

func TestAppendWritesValidJSONL(t *testing.T) {
	p := withTempHome(t)

	rec := restartlog.Record{
		TS:      time.Date(2026, 6, 29, 15, 54, 45, 0, time.UTC),
		Event:   restartlog.EventInitiated,
		From:    "inst_test_old",
		Reason:  "idle",
		Idle:    "30m0s",
		Backoff: "2s",
		PID:     4212,
	}
	if err := restartlog.Append(rec); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty restart log")
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("expected trailing newline, got %q", data)
	}

	var got restartlog.Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSONL: %v (raw: %s)", err, data)
	}
	if got.Event != restartlog.EventInitiated || got.From != "inst_test_old" || got.PID != 4212 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !got.TS.Equal(rec.TS) {
		t.Errorf("TS mismatch: got %s want %s", got.TS, rec.TS)
	}
}

func TestAppendPreservesOrder(t *testing.T) {
	p := withTempHome(t)

	events := []restartlog.Event{
		restartlog.EventInitiated,
		restartlog.EventExecFailed,
		restartlog.EventRestarted,
		restartlog.EventConfirmed,
	}
	for i, ev := range events {
		rec := restartlog.Record{
			TS:    time.Date(2026, 6, 29, 15, 0, i, 0, time.UTC),
			Event: ev,
			From:  "inst_test_old",
		}
		if err := restartlog.Append(rec); err != nil {
			t.Fatalf("Append #%d failed: %v", i, err)
		}
	}

	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var seen []restartlog.Event
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec restartlog.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("invalid JSONL line: %v (raw: %s)", err, line)
		}
		seen = append(seen, rec.Event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(seen) != len(events) {
		t.Fatalf("expected %d records, got %d (%v)", len(events), len(seen), seen)
	}
	for i, want := range events {
		if seen[i] != want {
			t.Errorf("record[%d].Event = %q, want %q", i, seen[i], want)
		}
	}
}

func TestAppendConcurrentDoesNotInterleave(t *testing.T) {
	p := withTempHome(t)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = restartlog.Append(restartlog.Record{
				TS:       time.Now(),
				Event:    restartlog.EventRestarted,
				Instance: "inst_test_concurrent",
				PID:      i,
			})
		}(i)
	}
	wg.Wait()

	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	seen := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec restartlog.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("interleaved or invalid line: %v\nraw: %s", err, line)
		}
		seen++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if seen != n {
		t.Errorf("expected %d intact records, got %d", n, seen)
	}
}

func TestAppendOmitsEmptyFields(t *testing.T) {
	p := withTempHome(t)

	if err := restartlog.Append(restartlog.Record{
		TS:       time.Date(2026, 6, 29, 15, 54, 45, 0, time.UTC),
		Event:    restartlog.EventConfirmed,
		Instance: "inst_test_only",
	}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	s := string(data)
	for _, omitted := range []string{`"from"`, `"reason"`, `"idle"`, `"error"`, `"pid"`} {
		if strings.Contains(s, omitted) {
			t.Errorf("expected %s to be omitted, raw: %s", omitted, s)
		}
	}
}
