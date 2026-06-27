package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
)

// captureStdout captures stdout during command execution
func captureStdout(f func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()

	w.Close()
	os.Stdout = oldStdout

	buf := &bytes.Buffer{}
	io.Copy(buf, r)
	return buf.String(), err
}

func TestNewCleanCommand(t *testing.T) {
	cmd := NewCleanCommand()

	if cmd.Use != "clean" {
		t.Errorf("expected Use %q, got %q", "clean", cmd.Use)
	}

	// Verify --all flag exists
	f := cmd.Flags().Lookup("all")
	if f == nil {
		t.Fatal("expected --all flag to exist")
	}
	if f.Shorthand != "a" {
		t.Errorf("expected --all shorthand 'a', got %q", f.Shorthand)
	}
	if f.DefValue != "false" {
		t.Errorf("expected --all default %q, got %q", "false", f.DefValue)
	}
}

func TestRunClean_NoInstances(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Execute clean command and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "No instances to clean") {
		t.Errorf("expected 'No instances to clean', got %q", output)
	}
}

func TestRunClean_StaleInstancesOnly(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create stale instance (PID 0 means not running)
	staleMeta := &daemon.InstanceMetadata{
		ID:         "inst_stale_test",
		Port:       8081,
		PID:        0, // PID 0 = not running
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(staleMeta); err != nil {
		t.Fatalf("failed to save stale instance: %v", err)
	}

	// Verify file exists
	instancesDir := filepath.Join(tmpDir, ".cc-modelrouter", "instances")
	filePath := filepath.Join(instancesDir, "inst_stale_test.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("expected stale instance file to exist before clean")
	}

	// Execute clean command and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "Removed stale instance") {
		t.Errorf("expected 'Removed stale instance', got %q", output)
	}

	// Verify file was deleted
	if _, err := os.Stat(filePath); err == nil {
		t.Error("expected stale instance file to be deleted")
	}
}

func TestRunClean_RunningInstanceNotCleaned(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create running instance (use current process PID)
	runningMeta := &daemon.InstanceMetadata{
		ID:         "inst_running_test",
		Port:       8082,
		PID:        os.Getpid(), // Current process PID = running
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(runningMeta); err != nil {
		t.Fatalf("failed to save running instance: %v", err)
	}

	// Verify file exists
	instancesDir := filepath.Join(tmpDir, ".cc-modelrouter", "instances")
	filePath := filepath.Join(instancesDir, "inst_running_test.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("expected running instance file to exist before clean")
	}

	// Execute clean command and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "No stale instances to remove") {
		t.Errorf("expected 'No stale instances to remove', got %q", output)
	}

	// Verify file still exists (running instance should NOT be deleted)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("expected running instance file to still exist")
	}

	// Cleanup: delete the instance file manually
	daemon.DeleteInstance("inst_running_test")
}

func TestRunClean_CleanAll(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create running instance (use current process PID)
	runningMeta := &daemon.InstanceMetadata{
		ID:         "inst_running_all_test",
		Port:       8083,
		PID:        os.Getpid(), // Current process PID = running
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(runningMeta); err != nil {
		t.Fatalf("failed to save running instance: %v", err)
	}

	// Create stale instance
	staleMeta := &daemon.InstanceMetadata{
		ID:         "inst_stale_all_test",
		Port:       8084,
		PID:        0, // PID 0 = not running
		ConfigType: "global",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(staleMeta); err != nil {
		t.Fatalf("failed to save stale instance: %v", err)
	}

	// Execute clean command with --all flag and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		cmd.SetArgs([]string{"--all"})
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	// Should show 2 removed instances
	if !strings.Contains(output, "Removed 2") {
		t.Errorf("expected 'Removed 2', got %q", output)
	}

	// Verify both files were deleted
	instancesDir := filepath.Join(tmpDir, ".cc-modelrouter", "instances")
	for _, id := range []string{"inst_running_all_test", "inst_stale_all_test"} {
		filePath := filepath.Join(instancesDir, id+".json")
		if _, err := os.Stat(filePath); err == nil {
			t.Errorf("expected instance file %s to be deleted with --all", id)
		}
	}
}

func TestRunClean_MixedInstances(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create stale instance (PID 0)
	staleMeta := &daemon.InstanceMetadata{
		ID:         "inst_mixed_stale",
		Port:       8085,
		PID:        0,
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(staleMeta); err != nil {
		t.Fatalf("failed to save stale instance: %v", err)
	}

	// Create running instance (current PID)
	runningMeta := &daemon.InstanceMetadata{
		ID:         "inst_mixed_running",
		Port:       8086,
		PID:        os.Getpid(),
		ConfigType: "global",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(runningMeta); err != nil {
		t.Fatalf("failed to save running instance: %v", err)
	}

	// Execute clean command without --all and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	// Should show 1 removed instance (only stale)
	if !strings.Contains(output, "Removed 1") {
		t.Errorf("expected 'Removed 1', got %q", output)
	}

	instancesDir := filepath.Join(tmpDir, ".cc-modelrouter", "instances")

	// Stale should be deleted
	stalePath := filepath.Join(instancesDir, "inst_mixed_stale.json")
	if _, err := os.Stat(stalePath); err == nil {
		t.Error("expected stale instance to be deleted")
	}

	// Running should still exist
	runningPath := filepath.Join(instancesDir, "inst_mixed_running.json")
	if _, err := os.Stat(runningPath); os.IsNotExist(err) {
		t.Error("expected running instance to still exist")
	}

	// Cleanup running instance
	daemon.DeleteInstance("inst_mixed_running")
}

func TestRunClean_NonExistentPID(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set HOME to temp directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create instance with non-existent PID (99999999 is unlikely to exist)
	deadMeta := &daemon.InstanceMetadata{
		ID:         "inst_dead_pid",
		Port:       8087,
		PID:        99999999, // Very unlikely to exist
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(deadMeta); err != nil {
		t.Fatalf("failed to save dead instance: %v", err)
	}

	// Execute clean command and capture output
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		return cmd.Execute()
	})

	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	// Should show that instance was removed (non-existent PID = stale)
	if !strings.Contains(output, "Removed") {
		t.Errorf("expected 'Removed', got %q", output)
	}

	// Verify file was deleted
	instancesDir := filepath.Join(tmpDir, ".cc-modelrouter", "instances")
	filePath := filepath.Join(instancesDir, "inst_dead_pid.json")
	if _, err := os.Stat(filePath); err == nil {
		t.Error("expected dead PID instance to be deleted")
	}
}

func TestNewCleanCommand_UsageFlags(t *testing.T) {
	cmd := NewCleanCommand()

	// Verify --usage-before flag exists
	f := cmd.Flags().Lookup("usage-before")
	if f == nil {
		t.Fatal("expected --usage-before flag to exist")
	}
	if f.DefValue != "" {
		t.Errorf("expected --usage-before default empty, got %q", f.DefValue)
	}

	// Verify --usage-all flag exists
	f = cmd.Flags().Lookup("usage-all")
	if f == nil {
		t.Fatal("expected --usage-all flag to exist")
	}
	if f.DefValue != "false" {
		t.Errorf("expected --usage-all default false, got %q", f.DefValue)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		wantH    int
		wantErr  bool
	}{
		{"30d", 30 * 24, false},
		{"7d", 7 * 24, false},
		{"24h", 24, false},
		{"90m", 1, false},   // 90m = 1.5h, truncated to int
		{"1h", 1, false},
		{"0d", 0, false},
		{"abc", 0, true},
		{"", 0, true},
		{"30", 0, true},
		{"30x", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := parseDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			gotH := int(d.Hours())
			if gotH != tt.wantH {
				t.Errorf("parseDuration(%q) = %d hours, want %d", tt.input, gotH, tt.wantH)
			}
		})
	}
}

func TestRunClean_UsageBefore(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create usage database with old and new records
	dbPath := filepath.Join(tmpDir, ".cc-modelrouter", "usage.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	db.Close()

	// Insert records directly
	db, err = usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	now := time.Now()
	records := []*usage.Record{
		{InstanceID: "inst1", Profile: "", Provider: "", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0, Timestamp: now.Add(-60 * 24 * time.Hour)},
		{InstanceID: "inst2", Profile: "", Provider: "", Route: "/default", Model: "m2", Tokens: 200, Fallbacks: 0, Timestamp: now},
	}
	for _, r := range records {
		if err := usage.InsertRecord(db, r); err != nil {
			t.Fatalf("InsertRecord failed: %v", err)
		}
	}
	db.Close()

	// Run clean with --usage-before 30d
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		cmd.SetArgs([]string{"--usage-before", "30d"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "Pruned 1 usage record(s)") {
		t.Errorf("expected 'Pruned 1 usage record(s)', got %q", output)
	}

	// Verify 1 record remains
	db, err = usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM usage_records").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 remaining record, got %d", count)
	}
}

func TestRunClean_UsageAll(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create usage database with records
	dbPath := filepath.Join(tmpDir, ".cc-modelrouter", "usage.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := usage.InsertRecord(db, &usage.Record{
			InstanceID: "inst1", Profile: "", Provider: "", Route: "/think", Model: "m1",
			Tokens: 100, Fallbacks: 0, Timestamp: now.Add(-time.Duration(i) * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("InsertRecord failed: %v", err)
		}
	}
	db.Close()

	// Run clean with --usage-all
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		cmd.SetArgs([]string{"--usage-all"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "Deleted 3 usage record(s)") {
		t.Errorf("expected 'Deleted 3 usage record(s)', got %q", output)
	}

	// Verify no records remain
	db, err = usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM usage_records").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 remaining records, got %d", count)
	}
}

func TestRunClean_UsageAllWinsOverBefore(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create usage database
	dbPath := filepath.Join(tmpDir, ".cc-modelrouter", "usage.db")
	db, err := usage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := usage.InsertRecord(db, &usage.Record{
		InstanceID: "inst1", Profile: "", Provider: "", Route: "/think", Model: "m1",
		Tokens: 100, Fallbacks: 0, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("InsertRecord failed: %v", err)
	}
	db.Close()

	// Both flags: --usage-all should win, --usage-before ignored
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		cmd.SetArgs([]string{"--usage-before", "30d", "--usage-all"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "Deleted 1 usage record(s)") {
		t.Errorf("expected 'Deleted 1 usage record(s)', got %q", output)
	}
	if strings.Contains(output, "Pruned") {
		t.Error("expected --usage-all to win, but got 'Pruned' in output")
	}
}

func TestRunClean_UsageBeforeNoDb(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// No usage database exists
	output, err := captureStdout(func() error {
		cmd := NewCleanCommand()
		cmd.SetArgs([]string{"--usage-before", "30d"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("clean command failed: %v", err)
	}

	if !strings.Contains(output, "No usage database found") {
		t.Errorf("expected 'No usage database found', got %q", output)
	}
}