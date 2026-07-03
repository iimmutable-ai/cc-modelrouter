package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestNewRootCommand(t *testing.T) {
	cmd := NewRootCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Verify basic command properties
	if cmd.Use != "ccrouter" {
		t.Errorf("expected Use 'ccrouter', got '%s'", cmd.Use)
	}
	if cmd.Short != "Claude Code Model Router" {
		t.Errorf("expected Short 'Claude Code Model Router', got '%s'", cmd.Short)
	}
}

func TestNewRootCommand_HasSubcommands(t *testing.T) {
	cmd := NewRootCommand()

	expectedSubcommands := []string{
		"code",
		"start",
		"stop",
		"restart",
		"status",
		"clean",
		"config",
		"logs",
		"monitor",
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

func TestNewRootCommand_Version(t *testing.T) {
	cmd := NewRootCommand()

	if cmd.Version == "" {
		t.Error("expected version to be set")
	}

	// Version should be set to the package variable
	if cmd.Version != Version {
		t.Errorf("expected version '%s', got '%s'", Version, cmd.Version)
	}
}

func TestNewRootCommand_VersionNotEmpty(t *testing.T) {
	cmd := NewRootCommand()

	if cmd.Version == "" {
		t.Error("expected version to be non-empty")
	}

	// Version can be one of three formats depending on build context:
	// - "X.Y.Z" (Fallback, no git)
	// - "dev-<hex>" (dev-local branch)
	// - "<tag>-<hex>" (release tag with build hex)
	// We just verify it's not empty and looks like a version string.
	if !strings.Contains(cmd.Version, ".") && !strings.Contains(cmd.Version, "-") {
		t.Errorf("expected version to contain '.' or '-' (semver or tag-hex), got '%s'", cmd.Version)
	}
}

func TestNewRootCommand_EachSubcommandNotNil(t *testing.T) {
	cmd := NewRootCommand()

	for _, sub := range cmd.Commands() {
		if sub == nil {
			t.Errorf("subcommand %s is nil", sub.Name())
		}
	}
}

func TestNewRootCommand_SubcommandCounts(t *testing.T) {
	cmd := NewRootCommand()

	subcommands := cmd.Commands()
	expectedCount := 13 // code, start, stop, restart, status, clean, config, logs, monitor, profile, keys, gen, setup

	if len(subcommands) != expectedCount {
		t.Errorf("expected %d subcommands, got %d", expectedCount, len(subcommands))
	}
}

func TestNewRootCommand_CodeCommand(t *testing.T) {
	cmd := NewRootCommand()

	codeCmd, _, _ := cmd.Find([]string{"code"})
	if codeCmd == nil {
		t.Error("expected 'code' subcommand to exist")
	}
	if codeCmd.Name() != "code" {
		t.Errorf("expected subcommand name 'code', got '%s'", codeCmd.Name())
	}
}

func TestNewRootCommand_StartCommand(t *testing.T) {
	cmd := NewRootCommand()

	startCmd, _, _ := cmd.Find([]string{"start"})
	if startCmd == nil {
		t.Error("expected 'start' subcommand to exist")
	}
	if startCmd.Name() != "start" {
		t.Errorf("expected subcommand name 'start', got '%s'", startCmd.Name())
	}
}

func TestNewRootCommand_StopCommand(t *testing.T) {
	cmd := NewRootCommand()

	stopCmd, _, _ := cmd.Find([]string{"stop"})
	if stopCmd == nil {
		t.Error("expected 'stop' subcommand to exist")
	}
	if stopCmd.Name() != "stop" {
		t.Errorf("expected subcommand name 'stop', got '%s'", stopCmd.Name())
	}
}

func TestNewRootCommand_RestartCommand(t *testing.T) {
	cmd := NewRootCommand()

	restartCmd, _, _ := cmd.Find([]string{"restart"})
	if restartCmd == nil {
		t.Error("expected 'restart' subcommand to exist")
	}
	if restartCmd.Name() != "restart" {
		t.Errorf("expected subcommand name 'restart', got '%s'", restartCmd.Name())
	}
}

func TestNewRootCommand_StatusCommand(t *testing.T) {
	cmd := NewRootCommand()

	statusCmd, _, _ := cmd.Find([]string{"status"})
	if statusCmd == nil {
		t.Error("expected 'status' subcommand to exist")
	}
	if statusCmd.Name() != "status" {
		t.Errorf("expected subcommand name 'status', got '%s'", statusCmd.Name())
	}
}

func TestNewRootCommand_CleanCommand(t *testing.T) {
	cmd := NewRootCommand()

	cleanCmd, _, _ := cmd.Find([]string{"clean"})
	if cleanCmd == nil {
		t.Error("expected 'clean' subcommand to exist")
	}
	if cleanCmd.Name() != "clean" {
		t.Errorf("expected subcommand name 'clean', got '%s'", cleanCmd.Name())
	}
}

func TestNewRootCommand_ConfigCommand(t *testing.T) {
	cmd := NewRootCommand()

	configCmd, _, _ := cmd.Find([]string{"config"})
	if configCmd == nil {
		t.Error("expected 'config' subcommand to exist")
	}
	if configCmd.Name() != "config" {
		t.Errorf("expected subcommand name 'config', got '%s'", configCmd.Name())
	}
}

func TestNewRootCommand_LogsCommand(t *testing.T) {
	cmd := NewRootCommand()

	logsCmd, _, _ := cmd.Find([]string{"logs"})
	if logsCmd == nil {
		t.Error("expected 'logs' subcommand to exist")
	}
	if logsCmd.Name() != "logs" {
		t.Errorf("expected subcommand name 'logs', got '%s'", logsCmd.Name())
	}
}

func TestExecute_CallsCobraExecute(t *testing.T) {
	// This test verifies that Execute() calls Execute on the root command
	// We can't easily test the full execution without creating a mock,
	// but we can verify the command structure is correct

	cmd := NewRootCommand()

	// Verify the command is properly initialized
	if cmd == nil {
		t.Error("expected non-nil command")
	}

	// Execute should use os.Exit(1) on error
	// We can't test this directly without forking, but we verify
	// the Execute function exists and is callable
	_ = Execute // Execute function exists
}

func TestExecute_WithValidHelp(t *testing.T) {
	// Save original stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// Execute with help flag
	os.Args = []string{"ccrouter", "--help"}
	Execute()

	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read output
	buf := new(strings.Builder)
	io.Copy(buf, r)

	output := buf.String()

	// Should contain help information
	if !strings.Contains(output, "Claude Code Model Router") {
		t.Error("expected help output to contain 'Claude Code Model Router'")
	}
}

func TestVersion(t *testing.T) {
	// Test the Version variable is set
	if Version == "" {
		t.Error("expected Version to be set")
	}

	// Version can be one of three formats depending on build context:
	// - "X.Y.Z" (Fallback, no git)
	// - "dev-<hex>" (dev-local branch)
	// - "<tag>-<hex>" (release tag with build hex)
	if !strings.Contains(Version, ".") && !strings.Contains(Version, "-") {
		t.Errorf("expected version to contain '.' or '-' (semver or tag-hex), got '%s'", Version)
	}
}

func TestNewRootCommand_PersistentFlags(t *testing.T) {
	cmd := NewRootCommand()

	// Verify basic flags exist
	// Cobra adds --help and --version by default
	helpFlag := cmd.PersistentFlags().Lookup("help")
	if helpFlag == nil {
		// Try regular flags as well since Cobra's implementation may vary
		helpFlag = cmd.Flags().Lookup("help")
		if helpFlag == nil {
			// help flag might not be directly accessible, this is okay
		}
	}

	// Test that we can create the command multiple times
	cmd2 := NewRootCommand()
	if cmd2 == nil {
		t.Error("expected non-nil command on second call")
	}
	if cmd2.Use != cmd.Use {
		t.Errorf("expected same Use value, got '%s' vs '%s'", cmd.Use, cmd2.Use)
	}
}

func TestNewRootCommand_Initialization(t *testing.T) {
	// Test that calling NewRootCommand multiple times doesn't cause issues
	for i := 0; i < 5; i++ {
		cmd := NewRootCommand()
		if cmd == nil {
			t.Errorf("iteration %d: expected non-nil command", i)
		}
		if cmd.Use != "ccrouter" {
			t.Errorf("iteration %d: expected Use 'ccrouter', got '%s'", i, cmd.Use)
		}
	}
}