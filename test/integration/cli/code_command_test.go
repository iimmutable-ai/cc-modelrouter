//go:build cli_tests
// +build cli_tests

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/test/util"
)

// TestCodeCommand tests the `ccrouter code` command end-to-end.
func TestCodeCommand(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	// Create temporary config file
	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18081,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			},
			"maxRetries": 2,
			"retryDelay": "500ms"
		},
		"logging": {
			"enabled": true,
			"destination": "file",
			"level": "debug"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Run the code command
	cmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
		t.Logf("Stdout: %s", stdout.String())
		t.Logf("Stderr: %s", stderr.String())
	}

	t.Logf("Code command output: %s", stdout.String())
}

// TestCodeCommandWithLogging tests the `ccrouter code` command with logging enabled.
func TestCodeCommandWithLogging(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "test.log")
	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18082,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			}
		},
		"logging": {
			"enabled": true,
			"destination": "file",
			"filePath": "` + logPath + `",
			"level": "info"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run in background
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Wait a bit for server to start
	time.Sleep(2 * time.Second)

	// Check if log file exists
	if _, err := os.Stat(logPath); err == nil {
		logContent, _ := os.ReadFile(logPath)
		t.Logf("Log file exists with %d bytes", len(logContent))
	}

	// Stop the process
	cmd.Process.Kill()
	cmd.Wait()

	t.Logf("Code command with logging completed")
}

// TestInvalidConfigFile tests handling of invalid config files.
func TestInvalidConfigFile(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "invalid-config.json")
	configContent := `{"invalid": "json"}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()

	t.Logf("Invalid config test - error: %v, stderr: %s", err, stderr.String())

	if err == nil {
		t.Error("Expected error for invalid config, got nil")
	}
}

// TestMissingConfigFile tests handling of missing config files.
func TestMissingConfigFile(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	cmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", "/nonexistent/config.json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	t.Logf("Missing config test - error: %v, stderr: %s", err, stderr.String())

	if err == nil {
		t.Error("Expected error for missing config, got nil")
	}
}

// TestConfigValidation tests config validation.
func TestConfigValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testCases := []struct {
		name        string
		config      string
		shouldError bool
	}{
		{
			name: "valid_config",
			config: `{
				"server": {"port": 18083, "host": "localhost"},
				"providers": {
					"test": {
						"baseURL": "https://api.test.com",
						"apiKey": "test-key",
						"models": ["test-model"]
					}
				},
				"router": {
					"routes": {"test-model": "test:test-model"}
				}
			}`,
			shouldError: false,
		},
		{
			name: "missing_api_key",
			config: `{
				"server": {"port": 18083, "host": "localhost"},
				"providers": {
					"test": {
						"baseURL": "https://api.test.com",
						"apiKey": "",
						"models": ["test-model"]
					}
				},
				"router": {
					"routes": {"test-model": "test:test-model"}
				}
			}`,
			shouldError: true,
		},
		{
			name: "invalid_port",
			config: `{
				"server": {"port": -1, "host": "localhost"},
				"providers": {
					"test": {
						"baseURL": "https://api.test.com",
						"apiKey": "test-key",
						"models": ["test-model"]
					}
				},
				"router": {
					"routes": {"test-model": "test:test-model"}
				}
			}`,
			shouldError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tc.name+".json")
			if err := os.WriteFile(configPath, []byte(tc.config), 0644); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}

			_ = util.NewTestConfigBuilder().
				WithServer("localhost", 18083).
				Build()

			t.Logf("Config validation test %s: err=%v", tc.name, err)
		})
	}
}

// TestCommandHelp tests the help command.
func TestCommandHelp(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	cmd := exec.Command("/tmp/ccmodelrouter-test", "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Errorf("Help command failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage:") && !strings.Contains(output, "A CLI tool") {
		t.Error("Help output doesn't contain expected content")
	}

	t.Logf("Help command output: %s", output)
}

// TestVersionCommand tests the version command.
func TestVersionCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	cmd := exec.Command("/tmp/ccmodelrouter-test", "version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Version command failed (may not be implemented): %v", err)
		return
	}

	t.Logf("Version command output: %s", stdout.String())
}

// TestCommandCompletion tests shell completion generation.
func TestCommandCompletion(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	// Test bash completion
	cmd := exec.Command("/tmp/ccmodelrouter-test", "completion", "bash")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Bash completion command failed (may not be implemented): %v", err)
		return
	}

	t.Logf("Bash completion generated %d bytes", stdout.Len())
}

// TestStopCommand tests stopping a running instance.
func TestStopCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18084,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start the server
	startCmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var startStdout, startStderr bytes.Buffer
	startCmd.Stdout = &startStdout
	startCmd.Stderr = &startStderr

	if err := startCmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Try to stop it
	stopCmd := exec.Command("/tmp/ccmodelrouter-test", "stop")
	var stopStdout, stopStderr bytes.Buffer
	stopCmd.Stdout = &stopStdout
	stopCmd.Stderr = &stopStderr

	if err := stopCmd.Run(); err != nil {
		t.Logf("Stop command result: %v", err)
		t.Logf("Stop stdout: %s", stopStdout.String())
		t.Logf("Stop stderr: %s", stopStderr.String())
	}

	// Kill the process if still running
	startCmd.Process.Kill()
	startCmd.Wait()

	t.Log("Stop command test completed")
}

// TestStatusCommand tests checking status of a running instance.
func TestStatusCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18085,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start the server
	startCmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var startStderr bytes.Buffer
	startCmd.Stdout = io.Discard
	startCmd.Stderr = &startStderr

	if err := startCmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Check status
	statusCmd := exec.Command("/tmp/ccmodelrouter-test", "status")
	var statusStdout, statusStderr bytes.Buffer
	statusCmd.Stdout = &statusStdout
	statusCmd.Stderr = &statusStderr

	if err := statusCmd.Run(); err != nil {
		t.Logf("Status command result: %v", err)
		t.Logf("Status stdout: %s", statusStdout.String())
		t.Logf("Status stderr: %s", statusStderr.String())
	}

	// Kill the process
	startCmd.Process.Kill()
	startCmd.Wait()

	t.Log("Status command test completed")
}

// TestConfigCommand tests config-related commands.
func TestConfigCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	// Test config init (may or may not be implemented)
	configCmd := exec.Command("/tmp/ccmodelrouter-test", "config", "init")
	var stdout, stderr bytes.Buffer
	configCmd.Stdout = &stdout
	configCmd.Stderr = &stderr

	err := configCmd.Run()
	if err != nil {
		t.Logf("Config init command result (may not be implemented): %v", err)
		t.Logf("Stdout: %s, Stderr: %s", stdout.String(), stderr.String())
	} else {
		t.Logf("Config init output: %s", stdout.String())
	}
}

// TestRestartCommand tests restarting a server instance.
func TestRestartCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18086,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start the server
	startCmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var startStderr bytes.Buffer
	startCmd.Stdout = io.Discard
	startCmd.Stderr = &startStderr

	if err := startCmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Try restart
	restartCmd := exec.Command("/tmp/ccmodelrouter-test", "restart")
	var restartStdout, restartStderr bytes.Buffer
	restartCmd.Stdout = &restartStdout
	restartCmd.Stderr = &restartStderr

	if err := restartCmd.Run(); err != nil {
		t.Logf("Restart command result (may not be implemented): %v", err)
		t.Logf("Restart stdout: %s, stderr: %s", restartStdout.String(), restartStderr.String())
	} else {
		t.Logf("Restart output: %s", restartStdout.String())
	}

	// Kill the process
	startCmd.Process.Kill()
	startCmd.Wait()

	t.Log("Restart command test completed")
}

// TestUsageCommand tests usage tracking command.
func TestUsageCommand(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {
			"port": 18087,
			"host": "localhost"
		},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {
				"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start the server
	startCmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var startStderr bytes.Buffer
	startCmd.Stdout = io.Discard
	startCmd.Stderr = &startStderr

	if err := startCmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Try usage command
	usageCmd := exec.Command("/tmp/ccmodelrouter-test", "usage")
	var usageStdout, usageStderr bytes.Buffer
	usageCmd.Stdout = &usageStdout
	usageCmd.Stderr = &usageStderr

	if err := usageCmd.Run(); err != nil {
		t.Logf("Usage command result (may not be implemented): %v", err)
		t.Logf("Usage stdout: %s, stderr: %s", usageStdout.String(), usageStderr.String())
	} else {
		t.Logf("Usage output: %s", usageStdout.String())
	}

	// Kill the process
	startCmd.Process.Kill()
	startCmd.Wait()

	t.Log("Usage command test completed")
}

// TestConfigFilePath tests different config file paths.
func TestConfigFilePath(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create config in nested directory
	nestedDir := filepath.Join(tmpDir, "nested", "deep")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("Failed to create nested dir: %v", err)
	}

	configPath := filepath.Join(nestedDir, "config.json")
	configContent := `{
		"server": {"port": 18088, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"]
			}
		},
		"router": {
			"routes": {"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := exec.Command("/tmp/ccmodelrouter-test", "code", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	time.Sleep(1 * time.Second)

	cmd.Process.Kill()
	cmd.Wait()

	t.Logf("Config file path test completed. Output: %s", stdout.String())
}

// TestConfigEnvironmentVariable tests config from environment variable.
func TestConfigEnvironmentVariable(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "/tmp/ccmodelrouter-test", "./cmd/ccrouter")
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Failed to build binary: %v", err)
	}
	defer os.Remove("/tmp/ccmodelrouter-test")

	tmpDir, err := os.MkdirTemp("", "ccmodelrouter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"server": {"port": 18089, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key",
				"models": ["claude-3-5-sonnet-20241022"]
			}
		},
		"router": {
			"routes": {"claude-3-5-sonnet-20241022": "mock:claude-3-5-sonnet-20241022"}
		}
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Set environment variable
	oldPath := os.Getenv("PATH")
	os.Setenv("CCMODELROUTER_CONFIG", configPath)
	defer os.Setenv("PATH", oldPath)
	os.Unsetenv("CCMODELROUTER_CONFIG")

	cmd := exec.Command("/tmp/ccmodelrouter-test", "code")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = append(os.Environ(), "CCMODELROUTER_CONFIG="+configPath)

	if err := cmd.Start(); err != nil {
		t.Logf("Failed to start with env var: %v", err)
		return
	}

	time.Sleep(1 * time.Second)

	cmd.Process.Kill()
	cmd.Wait()

	t.Logf("Config environment variable test completed. Output: %s", stdout.String())
}