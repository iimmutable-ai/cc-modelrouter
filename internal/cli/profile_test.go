package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/spf13/cobra"
)

func TestNewProfileCommand(t *testing.T) {
	cmd := NewProfileCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "profile" {
		t.Errorf("expected Use 'profile', got '%s'", cmd.Use)
	}

	if cmd.Short != "Manage route profiles" {
		t.Errorf("expected Short 'Manage route profiles', got '%s'", cmd.Short)
	}
}

func TestNewProfileCommand_HasSubcommands(t *testing.T) {
	cmd := NewProfileCommand()

	expectedSubcommands := []string{
		"list",
		"switch",
		"status",
	}

	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand '%s' to be present", expected)
		}
	}
}

func TestNewProfileCommand_SubcommandCount(t *testing.T) {
	cmd := NewProfileCommand()

	subcommands := cmd.Commands()
	expectedCount := 3 // list, switch, status

	if len(subcommands) != expectedCount {
		t.Errorf("expected %d subcommands, got %d", expectedCount, len(subcommands))
	}
}

func TestNewProfileListCommand(t *testing.T) {
	cmd := NewProfileListCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "list" {
		t.Errorf("expected Use 'list', got '%s'", cmd.Use)
	}

	if cmd.Short != "List all profiles" {
		t.Errorf("expected Short 'List all profiles', got '%s'", cmd.Short)
	}

	// Verify flags exist
	instanceFlag := cmd.Flags().Lookup("instance")
	if instanceFlag == nil {
		t.Error("expected 'instance' flag to exist")
	}

	fromConfigFlag := cmd.Flags().Lookup("from-config")
	if fromConfigFlag == nil {
		t.Error("expected 'from-config' flag to exist")
	}
}

func TestNewProfileSwitchCommand(t *testing.T) {
	cmd := NewProfileSwitchCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "switch <profile-name>" {
		t.Errorf("expected Use 'switch <profile-name>', got '%s'", cmd.Use)
	}

	if cmd.Short != "Switch to a profile" {
		t.Errorf("expected Short 'Switch to a profile', got '%s'", cmd.Short)
	}

	// Verify Args validation - should require exactly 1 arg
	if cmd.Args == nil {
		t.Error("expected Args validator to be set")
	}

	// Verify instance flag exists
	instanceFlag := cmd.Flags().Lookup("instance")
	if instanceFlag == nil {
		t.Error("expected 'instance' flag to exist")
	}
}

func TestNewProfileStatusCommand(t *testing.T) {
	cmd := NewProfileStatusCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "status" {
		t.Errorf("expected Use 'status', got '%s'", cmd.Use)
	}

	if cmd.Short != "Show active profile" {
		t.Errorf("expected Short 'Show active profile', got '%s'", cmd.Short)
	}

	// Verify instance flag exists
	instanceFlag := cmd.Flags().Lookup("instance")
	if instanceFlag == nil {
		t.Error("expected 'instance' flag to exist")
	}
}

func TestProfileCommand_IntegrationWithRoot(t *testing.T) {
	rootCmd := NewRootCommand()

	// Verify profile command is registered at root level
	profileCmd, _, _ := rootCmd.Find([]string{"profile"})
	if profileCmd == nil {
		t.Error("expected 'profile' command to be registered at root level")
	}

	if profileCmd.Name() != "profile" {
		t.Errorf("expected command name 'profile', got '%s'", profileCmd.Name())
	}

	// Verify subcommands are accessible
	listCmd, _, _ := rootCmd.Find([]string{"profile", "list"})
	if listCmd == nil {
		t.Error("expected 'profile list' subcommand to exist")
	}

	switchCmd, _, _ := rootCmd.Find([]string{"profile", "switch"})
	if switchCmd == nil {
		t.Error("expected 'profile switch' subcommand to exist")
	}

	statusCmd, _, _ := rootCmd.Find([]string{"profile", "status"})
	if statusCmd == nil {
		t.Error("expected 'profile status' subcommand to exist")
	}
}

// Nice-to-have: Tests for printProfileList output formatting

func TestPrintProfileList_WithProfiles(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"default": {
					Name:        "Fast",
					Description: "Fast models for quick responses",
					Routes:      map[string]string{"default": "provider:fast-model"},
				},
				"quality": {
					Name:        "Quality",
					Description: "High quality responses",
					Routes:      map[string]string{"default": "provider:quality-model"},
				},
			},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := printProfileList(cfg)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printProfileList failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output contains expected content
	// "default" is the default profile at startup
	if !strings.Contains(output, "Profiles (default at startup: default)") {
		t.Errorf("expected output to contain 'Profiles (default at startup: default)', got: %s", output)
	}
	if !strings.Contains(output, "* Fast [default]") {
		t.Errorf("expected output to show default profile with asterisk, got: %s", output)
	}
	if !strings.Contains(output, "Fast models for quick responses") {
		t.Errorf("expected output to contain description, got: %s", output)
	}
	if !strings.Contains(output, "Quality [quality]") {
		t.Errorf("expected output to show other profile without asterisk, got: %s", output)
	}
}

func TestPrintProfileList_NoProfiles_LegacyRoutes(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
				"think":   "provider:legacy-think",
			},
			Profiles: map[string]config.ProfileConfig{},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := printProfileList(cfg)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printProfileList failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output shows legacy routes
	if !strings.Contains(output, "No profiles configured") {
		t.Errorf("expected output to contain 'No profiles configured', got: %s", output)
	}
	if !strings.Contains(output, "Using legacy routes:") {
		t.Errorf("expected output to contain 'Using legacy routes:', got: %s", output)
	}
	if !strings.Contains(output, "default: provider:legacy-default") {
		t.Errorf("expected output to show legacy default route, got: %s", output)
	}
	if !strings.Contains(output, "think: provider:legacy-think") {
		t.Errorf("expected output to show legacy think route, got: %s", output)
	}
}

func TestPrintProfileList_ProfileWithoutDescription(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"default": {
					Name:        "Minimal",
					Description: "", // No description
					Routes:      map[string]string{"default": "provider:model"},
				},
			},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := printProfileList(cfg)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printProfileList failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should show profile without description suffix
	// "default" is the default profile at startup
	if !strings.Contains(output, "* Minimal [default]") {
		t.Errorf("expected output to show default profile without description suffix, got: %s", output)
	}
	// Should NOT have the " - " separator for empty description
	if strings.Contains(output, "Minimal [default] -") {
		t.Errorf("expected output NOT to have empty description with separator, got: %s", output)
	}
}

// Nice-to-have: Tests for findRunningInstance edge cases

func TestFindRunningInstance_NoInstances(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	_, err := findRunningInstance("")
	if err == nil {
		t.Error("expected error when no instances exist")
	}
	if !strings.Contains(err.Error(), "no running instances") {
		t.Errorf("expected error to mention 'no running instances', got: %v", err)
	}
}

func TestFindRunningInstance_SpecificInstanceNotFound(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create an instance but not the one we're looking for
	meta := &daemon.InstanceMetadata{
		ID:         "inst_exists",
		Port:       8081,
		PID:        os.Getpid(), // Current process is running
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	_, err := findRunningInstance("inst_nonexistent")
	if err == nil {
		t.Error("expected error when specific instance not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention 'not found', got: %v", err)
	}
}

func TestFindRunningInstance_MostRecent(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create multiple running instances with different start times
	now := time.Now()
	instances := []*daemon.InstanceMetadata{
		{
			ID:         "inst_old",
			Port:       8081,
			PID:        os.Getpid(),
			ConfigType: "project",
			StartTime:  now.Add(-2 * time.Hour),
		},
		{
			ID:         "inst_middle",
			Port:       8082,
			PID:        os.Getpid(),
			ConfigType: "global",
			StartTime:  now.Add(-1 * time.Hour),
		},
		{
			ID:         "inst_newest",
			Port:       8083,
			PID:        os.Getpid(),
			ConfigType: "project",
			StartTime:  now.Add(-10 * time.Minute), // Most recent
		},
	}

	for _, inst := range instances {
		if err := daemon.SaveInstance(inst); err != nil {
			t.Fatalf("failed to save instance %s: %v", inst.ID, err)
		}
	}

	// Find without specifying ID - should return most recent
	found, err := findRunningInstance("")
	if err != nil {
		t.Fatalf("findRunningInstance failed: %v", err)
	}

	if found.ID != "inst_newest" {
		t.Errorf("expected most recent instance 'inst_newest', got '%s'", found.ID)
	}
}

func TestFindRunningInstance_SpecificInstance(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create multiple running instances
	now := time.Now()
	instances := []*daemon.InstanceMetadata{
		{ID: "inst_a", Port: 8081, PID: os.Getpid(), ConfigType: "project", StartTime: now.Add(-1 * time.Hour)},
		{ID: "inst_b", Port: 8082, PID: os.Getpid(), ConfigType: "global", StartTime: now.Add(-30 * time.Minute)},
		{ID: "inst_c", Port: 8083, PID: os.Getpid(), ConfigType: "project", StartTime: now},
	}

	for _, inst := range instances {
		if err := daemon.SaveInstance(inst); err != nil {
			t.Fatalf("failed to save instance %s: %v", inst.ID, err)
		}
	}

	// Find specific instance
	found, err := findRunningInstance("inst_b")
	if err != nil {
		t.Fatalf("findRunningInstance failed: %v", err)
	}

	if found.ID != "inst_b" {
		t.Errorf("expected instance 'inst_b', got '%s'", found.ID)
	}
}

func TestFindRunningInstance_StoppedInstances(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create instances - but with PIDs that don't exist (stopped)
	meta := &daemon.InstanceMetadata{
		ID:         "inst_stopped",
		Port:       8081,
		PID:        99999999, // Non-existent PID
		ConfigType: "project",
		StartTime:  time.Now(),
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	_, err := findRunningInstance("")
	if err == nil {
		t.Error("expected error when all instances are stopped")
	}
	if !strings.Contains(err.Error(), "no running instances") {
		t.Errorf("expected error to mention 'no running instances', got: %v", err)
	}
}

func TestFindRunningInstance_MixedRunningStopped(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	now := time.Now()
	// Create mix of running and stopped instances
	instances := []*daemon.InstanceMetadata{
		{ID: "inst_stopped_old", Port: 8081, PID: 99999990, ConfigType: "project", StartTime: now.Add(-3 * time.Hour)},
		{ID: "inst_running", Port: 8082, PID: os.Getpid(), ConfigType: "global", StartTime: now.Add(-1 * time.Hour)},
		{ID: "inst_stopped_new", Port: 8083, PID: 99999991, ConfigType: "project", StartTime: now},
	}

	for _, inst := range instances {
		if err := daemon.SaveInstance(inst); err != nil {
			t.Fatalf("failed to save instance %s: %v", inst.ID, err)
		}
	}

	// Should find the running instance
	found, err := findRunningInstance("")
	if err != nil {
		t.Fatalf("findRunningInstance failed: %v", err)
	}

	if found.ID != "inst_running" {
		t.Errorf("expected running instance 'inst_running', got '%s'", found.ID)
	}
}

// --- Runtime function tests using httptest mock admin API ---

func TestListProfilesFromInstance_WithProfiles(t *testing.T) {
	adminToken := "test-admin-token"

	// Save and restore HOME
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create mock admin API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_admin/profiles" {
			token := r.URL.Query().Get("token")
			if token != adminToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			resp := map[string]interface{}{
				"profiles": []map[string]interface{}{
					{"key": "default", "name": "Default", "isActive": true},
					{"key": "fast", "name": "Fast", "description": "Fast models", "isActive": false},
				},
				"activeProfile": "default",
				"hasProfiles":   true,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Extract port from test server URL
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("failed to extract port: %v", err)
	}

	// Create instance pointing to test server
	meta := &daemon.InstanceMetadata{
		ID:           "inst_test_" + daemon.GenerateInstanceID(),
		Port:         port,
		PID:          os.Getpid(),
		ConfigType:   "project",
		StartTime:    time.Now(),
		AdminToken:   adminToken,
		ActiveProfile: "default",
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = listProfilesFromInstance(meta.ID)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("listProfilesFromInstance failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output
	if !strings.Contains(output, "Profiles (active: default)") {
		t.Errorf("expected output to contain profile list, got: %s", output)
	}
	if !strings.Contains(output, "* Default [default]") {
		t.Errorf("expected default profile marked as active, got: %s", output)
	}
	if !strings.Contains(output, "Fast [fast]") {
		t.Errorf("expected Fast profile, got: %s", output)
	}
}

func TestListProfilesFromInstance_LegacyRoutes(t *testing.T) {
	adminToken := "test-admin-token"

	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_admin/profiles" {
			resp := map[string]interface{}{
				"activeProfile": "",
				"hasProfiles":   false,
				"legacyRoutes": map[string]string{
					"default": "legacy:default-model",
					"think":   "legacy:think-model",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port, _ := extractPort(server.URL)

	meta := &daemon.InstanceMetadata{
		ID:         "inst_legacy_" + daemon.GenerateInstanceID(),
		Port:       port,
		PID:        os.Getpid(),
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: adminToken,
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listProfilesFromInstance(meta.ID)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("listProfilesFromInstance failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "No profiles configured") {
		t.Errorf("expected 'No profiles configured' for legacy routes, got: %s", output)
	}
	if !strings.Contains(output, "legacy:default-model") {
		t.Errorf("expected legacy route in output, got: %s", output)
	}
}

func TestRunProfileSwitch_Success(t *testing.T) {
	adminToken := "test-admin-token"

	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_admin/profiles/switch" && r.Method == http.MethodPost {
			token := r.Header.Get("X-Admin-Token")
			if token != adminToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var body struct {
				Profile string `json:"profile"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			resp := map[string]interface{}{
				"success":       true,
				"activeProfile":  body.Profile,
				"profileName":   "fast",
				"routes":        map[string]string{"default": "test:fast-model"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port, _ := extractPort(server.URL)

	meta := &daemon.InstanceMetadata{
		ID:           "inst_switch_" + daemon.GenerateInstanceID(),
		Port:         port,
		PID:          os.Getpid(),
		ConfigType:   "project",
		StartTime:    time.Now(),
		AdminToken:   adminToken,
		ActiveProfile: "default",
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run with a cobra.Command mock
	cmd := &cobra.Command{}
	cmd.Flags().String("instance", meta.ID, "")

	err := runProfileSwitch(cmd, []string{"fast"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runProfileSwitch failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Switched to profile:") {
		t.Errorf("expected switch confirmation, got: %s", output)
	}
	if !strings.Contains(output, "test:fast-model") {
		t.Errorf("expected route in output, got: %s", output)
	}
}

func TestRunProfileStatus_NoProfiles(t *testing.T) {
	adminToken := "test-admin-token"

	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_admin/profiles/active" {
			resp := map[string]interface{}{
				"activeProfile": "",
				"hasProfiles":   false,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port, _ := extractPort(server.URL)

	meta := &daemon.InstanceMetadata{
		ID:         "inst_status_" + daemon.GenerateInstanceID(),
		Port:       port,
		PID:        os.Getpid(),
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: adminToken,
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.Flags().String("instance", meta.ID, "")

	err := runProfileStatus(cmd, nil)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runProfileStatus failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "No profiles configured") {
		t.Errorf("expected no profiles message, got: %s", output)
	}
}

func TestRunProfileStatus_AdminError(t *testing.T) {
	adminToken := "test-admin-token"

	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	port, _ := extractPort(server.URL)

	meta := &daemon.InstanceMetadata{
		ID:         "inst_status_err_" + daemon.GenerateInstanceID(),
		Port:       port,
		PID:        os.Getpid(),
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: adminToken,
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("instance", meta.ID, "")

	err := runProfileStatus(cmd, nil)
	if err == nil {
		t.Fatal("expected error for non-200 admin response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status 500 error, got: %v", err)
	}
}

func TestRunProfileSwitch_NoInstances(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cmd := &cobra.Command{}
	cmd.Flags().String("instance", "", "")

	err := runProfileSwitch(cmd, []string{"fast"})
	if err == nil {
		t.Fatal("expected error for no instances")
	}
	if !strings.Contains(err.Error(), "no running instances") {
		t.Errorf("expected 'no running instances' error, got: %v", err)
	}
}

func TestRunProfileSwitch_NonRunningInstance(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create a stale instance (PID 0)
	meta := &daemon.InstanceMetadata{
		ID:         "inst_stale_switch_" + daemon.GenerateInstanceID(),
		Port:       9999,
		PID:        0,
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: "test-token",
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("instance", meta.ID, "")

	err := runProfileSwitch(cmd, []string{"fast"})
	if err == nil {
		t.Fatal("expected error for non-running instance")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected 'not running' error, got: %v", err)
	}
}

func TestRunProfileSwitch_InvalidProfile(t *testing.T) {
	adminToken := "test-admin-token"

	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_admin/profiles/switch" {
			resp := map[string]interface{}{
				"success": false,
				"error":   "profile not found: nonexistent",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port, _ := extractPort(server.URL)

	meta := &daemon.InstanceMetadata{
		ID:         "inst_err_" + daemon.GenerateInstanceID(),
		Port:       port,
		PID:        os.Getpid(),
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: adminToken,
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("instance", meta.ID, "")

	err := runProfileSwitch(cmd, []string{"nonexistent"})

	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
	if !strings.Contains(err.Error(), "profile not found") {
		t.Errorf("expected 'profile not found' error, got: %v", err)
	}
}

func TestListProfilesFromInstance_NotRunning(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create stopped instance
	meta := &daemon.InstanceMetadata{
		ID:         "inst_stopped_test",
		Port:       8081,
		PID:        99999999, // Non-existent
		ConfigType: "project",
		StartTime:  time.Now(),
		AdminToken: "test-token",
	}
	if err := daemon.SaveInstance(meta); err != nil {
		t.Fatalf("failed to save instance: %v", err)
	}

	err := listProfilesFromInstance(meta.ID)
	if err == nil {
		t.Fatal("expected error for stopped instance")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected 'not running' error, got: %v", err)
	}
}

// Helper to extract port from httptest URL (e.g., "http://127.0.0.1:45231" -> 45231)
func extractPort(url string) (int, error) {
	parts := strings.Split(url, ":")
	portStr := parts[len(parts)-1]
	return strconv.Atoi(portStr)
}
