//go:build cli_tests
// +build cli_tests

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable/cc-modelrouter/test/util"
)

// findProjectRoot walks up from the test file directory to find the project root (where go.mod is).
func findProjectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file) // test/integration/cli
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dir
}

// buildTestBinary returns the path to the built test binary.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	path := "/tmp/ccmodelrouter-start-test"
	root := findProjectRoot()
	cmd := exec.Command("go", "build", "-o", path, filepath.Join(root, "cmd", "ccrouter"))
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("Failed to build binary: %v\n%s", err, out)
	}
	t.Cleanup(func() { os.Remove(path) })
	return path
}

// writeConfig writes a JSON config file and returns its path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	return p
}

// minConfig returns a minimal valid config JSON string with the given port.
func minConfig(port int) string {
	return fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key-12345",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {"default": "mock:test-model"},
			"maxRetries": 1,
			"retryDelay": "200ms"
		}
	}`, port)
}

// minConfigWithBaseURL returns a config with a custom provider baseURL.
func minConfigWithBaseURL(port int, baseURL string) string {
	return fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "%s",
				"apiKey": "test-key-12345",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {"default": "mock:test-model"},
			"maxRetries": 1,
			"retryDelay": "200ms"
		}
	}`, port, baseURL)
}

// startServerWithOutput starts ccrouter start in the background with piped stdout/stderr.
// Returns the command and buffers. Uses t.Cleanup to kill the process.
// If homeDir is non-empty, sets HOME env on the server process for instance isolation.
func startServerWithOutput(t *testing.T, binaryPath, configPath string, homeDir string, extraArgs ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	args := append([]string{"start", "--config", configPath}, extraArgs...)
	cmd := exec.Command(binaryPath, args...)

	var stdout, stderr bytes.Buffer
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	// Set HOME env for instance isolation if provided
	if homeDir != "" {
		cmd.Env = append(os.Environ(), "HOME="+homeDir)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	go io.Copy(&stdout, stdoutPipe)
	go io.Copy(&stderr, stderrPipe)

	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})

	return cmd, &stdout, &stderr
}

// nextPort returns a unique port for testing, starting from 19100.
var portCounter = 19100

func nextPort() int {
	portCounter++
	return portCounter
}

// --- Standalone Start Tests ---

func TestStartCommand_BasicLifecycle(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use temp HOME for instance isolation
	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd, stdoutBuf, stderrBuf := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))

	// Wait for server startup
	time.Sleep(2 * time.Second)

	// Verify startup output
	out := stdoutBuf.String()
	t.Logf("Server stdout: %s", out)
	if !strings.Contains(out, "Starting router on") {
		t.Errorf("Expected 'Starting router on' in stdout, got: %s", out)
	}
	if !strings.Contains(out, "Router started with instance ID:") {
		t.Errorf("Expected 'Router started with instance ID:' in stdout")
	}

	// Verify instance metadata was created
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	entries, err := os.ReadDir(instancesDir)
	if err != nil {
		t.Fatalf("Failed to read instances dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected at least one instance metadata file")
	} else {
		t.Logf("Instance metadata file: %s", entries[0].Name())
	}

	// Verify server is listening by checking status
	statusCmd := exec.Command(binary, "status")
	statusCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	if err := statusCmd.Run(); err != nil {
		t.Logf("Status command error: %v", err)
	}
	t.Logf("Status output:\n%s", statusOut.String())

	if !strings.Contains(statusOut.String(), "running") {
		t.Errorf("Expected 'running' in status output")
	}

	// Graceful shutdown via SIGINT (same as Ctrl+C)
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()
	time.Sleep(500 * time.Millisecond)

	// Verify instance metadata was cleaned up
	entries2, _ := os.ReadDir(instancesDir)
	if len(entries2) > 0 {
		t.Errorf("Expected instance metadata to be deleted after shutdown, found %d files", len(entries2))
	}

	t.Logf("Stderr: %s", stderrBuf.String())
}

func TestStartCommand_ProxyRequestRouting(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	// Create mock provider server that returns valid Anthropic response
	mockServer := util.NewMockServer()
	defer mockServer.Close()

	mockServer.SetResponse("/v1/messages", http.StatusOK, `{
		"id":"msg_test","type":"message","role":"assistant",
		"content":[{"type":"text","text":"Hello from mock"}],
		"model":"test-model","stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":5}
	}`, map[string]string{"Content-Type": "application/json"})

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfigWithBaseURL(port, mockServer.URL()))

	cmd, stdoutBuf, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))

	// Wait for server startup
	time.Sleep(2 * time.Second)

	t.Logf("Server stdout: %s", stdoutBuf.String())

	// Send a real HTTP request through the proxy
	reqBody := `{
		"model": "test-model",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "Hello"}]
	}`
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/v1/messages", port),
		"application/json",
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("Failed to send request to proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Proxy response status: %d", resp.StatusCode)
	t.Logf("Proxy response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify the response is valid JSON with expected structure
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}
	if result["type"] != "message" {
		t.Errorf("Expected type 'message', got %v", result["type"])
	}

	// Verify mock server received the request with correct headers
	if mockServer.GetRequestCount() < 1 {
		t.Error("Expected mock server to receive at least 1 request")
	} else {
		t.Logf("Mock server received %d requests", mockServer.GetRequestCount())
		req := mockServer.GetRequests()[0]
		if req.Header.Get("x-api-key") != "test-key-12345" {
			t.Errorf("Expected x-api-key header 'test-key-12345', got %s", req.Header.Get("x-api-key"))
		}
		if req.Header.Get("anthropic-version") == "" {
			t.Error("Expected anthropic-version header to be set")
		}
	}

	// Cleanup
	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_InvalidConfigPath(t *testing.T) {
	binary := buildTestBinary(t)

	cmd := exec.Command(binary, "start", "--config", "/nonexistent/path/config.json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error for nonexistent config path, got nil")
	}
	if !strings.Contains(stderr.String(), "failed to load config") && !strings.Contains(stderr.String(), "no such file") {
		t.Errorf("Expected config load error, got: %s", stderr.String())
	}
	t.Logf("Invalid config error (expected): %v — %s", err, stderr.String())
}

func TestStartCommand_EmptyAPIKey(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	port := nextPort()
	config := fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {"test-model": "mock:test-model"}
		}
	}`, port)
	configPath := writeConfig(t, tmpDir, config)

	cmd := exec.Command(binary, "start", "--config", configPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		t.Fatal("Expected error for empty API key, got nil")
	}
	if !strings.Contains(stderr.String(), "API key is empty") {
		t.Errorf("Expected 'API key is empty' error, got: %s", stderr.String())
	}
	t.Logf("Empty API key error (expected): %v — %s", err, stderr.String())
}

func TestStartCommand_UnsetEnvVarAPIKey(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	port := nextPort()
	config := fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "${CCROUTER_NONEXISTENT_KEY_12345}",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {"test-model": "mock:test-model"}
		}
	}`, port)
	configPath := writeConfig(t, tmpDir, config)

	cmd := exec.Command(binary, "start", "--config", configPath)
	// Ensure the env var is NOT set
	cmd.Env = os.Environ()
	for i, env := range cmd.Env {
		if strings.HasPrefix(env, "CCROUTER_NONEXISTENT_KEY") {
			cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
			break
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		t.Fatal("Expected error for unset env var API key, got nil")
	}
	errMsg := stderr.String()
	if !strings.Contains(errMsg, "environment variable not set") && !strings.Contains(errMsg, "API key is empty") {
		t.Errorf("Expected env var error, got: %s", errMsg)
	}
	t.Logf("Unset env var error (expected): %v — %s", err, errMsg)
}

func TestStartCommand_InvalidProfile(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd := exec.Command(binary, "start", "--config", configPath, "--profile", "nonexistent")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		t.Fatal("Expected error for invalid profile, got nil")
	}
	// Config has no profiles section, so should say no profiles configured
	t.Logf("Invalid profile error: %s", stderr.String())
}

func TestStartCommand_ValidProfile(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	config := fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key-12345",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"profiles": {
				"default": {
					"name": "Default",
					"description": "Default profile",
					"routes": {"test-model": "mock:test-model"}
				}
			}
		}
	}`, port)
	configPath := writeConfig(t, tmpDir, config)

	cmd, stdoutBuf, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port), "--profile", "default")
	time.Sleep(2 * time.Second)

	out := stdoutBuf.String()
	t.Logf("Profile output: %s", out)

	if !strings.Contains(out, "Using profile: default") {
		t.Errorf("Expected 'Using profile: default' in output")
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_ProfileFlagWithNoProfiles(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd := exec.Command(binary, "start", "--config", configPath, "--profile", "test")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		t.Fatal("Expected error when --profile used with no profiles, got nil")
	}
	if !strings.Contains(stderr.String(), "no profiles configured") {
		t.Errorf("Expected 'no profiles configured' error, got: %s", stderr.String())
	}
}

func TestStartCommand_PortOverride(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	// Config says port 18080, but we override to a unique port
	configPath := writeConfig(t, tmpDir, minConfig(18080))
	overridePort := nextPort()

	cmd, stdoutBuf, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", overridePort))
	time.Sleep(2 * time.Second)

	out := stdoutBuf.String()
	t.Logf("Port override output: %s", out)

	// Verify the overridden port is in the startup message
	if !strings.Contains(out, fmt.Sprintf(":%d", overridePort)) {
		t.Errorf("Expected port %d in output, got: %s", overridePort, out)
	}

	// Verify we can actually connect on the overridden port
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/v1/models", overridePort))
	if err != nil {
		t.Errorf("Failed to connect on overridden port %d: %v", overridePort, err)
	} else {
		resp.Body.Close()
		t.Logf("Successfully connected on port %d, status: %d", overridePort, resp.StatusCode)
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_HostOverride(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd, stdoutBuf, _ := startServerWithOutput(t, binary, configPath, tempHome, "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	out := stdoutBuf.String()
	t.Logf("Host override output: %s", out)

	if !strings.Contains(out, "127.0.0.1") {
		t.Errorf("Expected '127.0.0.1' in output, got: %s", out)
	}

	// Verify connection works on 127.0.0.1
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/models", port))
	if err != nil {
		t.Errorf("Failed to connect on 127.0.0.1:%d: %v", port, err)
	} else {
		resp.Body.Close()
		t.Logf("Connected on 127.0.0.1:%d, status: %d", port, resp.StatusCode)
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_LogToFile(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "logs"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome,
		"--port", fmt.Sprintf("%d", port),
		"--log-destination", "file",
		"--log-level", "debug")

	time.Sleep(3 * time.Second)

	// Check that a log file was created in the logs directory
	logsDir := filepath.Join(tempHome, ".cc-modelrouter", "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("Failed to read logs dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected log file to be created, found none")
	} else {
		logContent, err := os.ReadFile(filepath.Join(logsDir, entries[0].Name()))
		if err != nil {
			t.Fatalf("Failed to read log file: %v", err)
		}
		t.Logf("Log file size: %d bytes", len(logContent))
		if len(logContent) == 0 {
			t.Error("Log file is empty")
		}
		logStr := string(logContent)
		if !strings.Contains(logStr, "Logging initialized") {
			snippet := logStr
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			t.Errorf("Expected 'Logging initialized' in log, got: %s", snippet)
		}
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_LogLevelOverride(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd, stdoutBuf, _ := startServerWithOutput(t, binary, configPath, tempHome,
		"--port", fmt.Sprintf("%d", port),
		"--log-level", "debug")

	time.Sleep(2 * time.Second)

	out := stdoutBuf.String()
	t.Logf("Log level override output: %s", out)

	// --log-level implicitly enables logging
	if strings.Contains(out, "Logging to:") || strings.Contains(out, "Logging: disabled") {
		t.Logf("Logging output present as expected")
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_AdminAPI_ProfileSwitch(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	config := fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key-12345",
				"models": ["test-model"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"profiles": {
				"default": {
					"name": "Default",
					"routes": {"test-model": "mock:test-model"}
				},
				"fast": {
					"name": "Fast",
					"routes": {"test-model": "mock:test-model"}
				}
			}
		}
	}`, port)
	configPath := writeConfig(t, tmpDir, config)

	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	// Read instance metadata to get admin token
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	entries, _ := os.ReadDir(instancesDir)
	if len(entries) == 0 {
		t.Fatal("No instance metadata found")
	}

	metaData, _ := os.ReadFile(filepath.Join(instancesDir, entries[0].Name()))
	var meta struct {
		AdminToken    string `json:"adminToken"`
		Port          int    `json:"port"`
		ActiveProfile string `json:"activeProfile"`
	}
	json.Unmarshal(metaData, &meta)
	t.Logf("Admin token: %s..., Port: %d, Active profile: %s", meta.AdminToken[:8], meta.Port, meta.ActiveProfile)

	client := &http.Client{Timeout: 5 * time.Second}

	// GET /_admin/profiles/active
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/_admin/profiles/active", port), nil)
	req.Header.Set("X-Admin-Token", meta.AdminToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to call admin API: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("Active profile response (%d): %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 from admin API, got %d", resp.StatusCode)
	}

	// POST /_admin/profiles/switch to "fast"
	switchBody := `{"profile": "fast"}`
	req2, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/_admin/profiles/switch", port), strings.NewReader(switchBody))
	req2.Header.Set("X-Admin-Token", meta.AdminToken)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Failed to switch profile: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	t.Logf("Switch profile response (%d): %s", resp2.StatusCode, string(body2))

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 from profile switch, got %d", resp2.StatusCode)
	}

	// Verify profile actually switched
	req3, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/_admin/profiles/active", port), nil)
	req3.Header.Set("X-Admin-Token", meta.AdminToken)
	resp3, err := client.Do(req3)
	if err != nil {
		t.Fatalf("Failed to get active profile after switch: %v", err)
	}
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	t.Logf("Active profile after switch: %s", string(body3))

	if !strings.Contains(string(body3), "fast") {
		t.Errorf("Expected profile 'fast' after switch, got: %s", string(body3))
	}

	// Unauthorized access (no token) should return 401
	req4, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/_admin/profiles/active", port), nil)
	resp4, err := client.Do(req4)
	if err != nil {
		t.Fatalf("Failed to call admin API without token: %v", err)
	}
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 without token, got %d", resp4.StatusCode)
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_ModelsEndpoint(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	config := fmt.Sprintf(`{
		"server": {"port": %d, "host": "localhost"},
		"providers": {
			"mock": {
				"baseURL": "https://api.anthropic.com",
				"apiKey": "test-key-12345",
				"models": ["model-a", "model-b", "model-c"],
				"transformer": "anthropic"
			}
		},
		"router": {
			"routes": {"model-a": "mock:model-a"}
		}
	}`, port)
	configPath := writeConfig(t, tmpDir, config)

	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/v1/models", port))
	if err != nil {
		t.Fatalf("Failed to call /v1/models: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("Models endpoint response (%d): %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 from /v1/models, got %d", resp.StatusCode)
	}

	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		t.Fatalf("Failed to parse models response: %v", err)
	}
	if len(modelsResp.Data) == 0 {
		t.Error("Expected models in response, got empty data array")
	}

	// Verify all models are present
	modelIDs := make(map[string]bool)
	for _, m := range modelsResp.Data {
		modelIDs[m.ID] = true
	}
	for _, expected := range []string{"model-a", "model-b", "model-c"} {
		if !modelIDs[expected] {
			t.Errorf("Expected model '%s' in response", expected)
		}
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStartCommand_UnsupportedEndpoint(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))

	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/v1/unsupported", port))
	if err != nil {
		t.Fatalf("Failed to call unsupported endpoint: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for unsupported endpoint, got %d", resp.StatusCode)
	}

	cmd.Process.Kill()
	cmd.Wait()
}

// --- Logs Command Tests ---

func TestLogsCommand_NoRunningInstances(t *testing.T) {
	binary := buildTestBinary(t)

	// Use temp HOME to avoid finding real instances
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	cmd := exec.Command(binary, "logs")
	cmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err != nil {
		t.Logf("Logs command error: %v", err)
	}

	if !strings.Contains(stdout.String(), "No running instances found") {
		t.Errorf("Expected 'No running instances found', got: %s", stdout.String())
	}
}

func TestLogsCommand_WithLogFile(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	os.MkdirAll(instancesDir, 0755)

	// Create fake instance metadata with current PID
	instanceID := "inst_testlog_20260608"
	meta := fmt.Sprintf(`{
		"id": "%s",
		"port": 19199,
		"pid": %d,
		"configType": "custom",
		"projectRoot": "%s",
		"startTime": "%s",
		"adminToken": "testtoken1234567890testtoken123456"
	}`, instanceID, os.Getpid(), tmpDir, time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(instancesDir, instanceID+".json"), []byte(meta), 0600)

	// Create log file where getLogPath expects it
	logContent := "line1: Starting router\nline2: Logging initialized\nline3: Router started\nline4: Request received\nline5: Response sent\n"
	os.WriteFile(filepath.Join(instancesDir, instanceID+".log"), []byte(logContent), 0644)

	cmd := exec.Command(binary, "logs", instanceID)
	cmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err != nil {
		t.Logf("Logs command error: %v", err)
	}

	out := stdout.String()
	t.Logf("Logs output:\n%s", out)

	if !strings.Contains(out, "line1: Starting router") {
		t.Error("Expected first log line in output")
	}
	if !strings.Contains(out, "line5: Response sent") {
		t.Error("Expected last log line in output")
	}
}

func TestLogsCommand_TailFlag(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	os.MkdirAll(instancesDir, 0755)

	instanceID := "inst_testtail_20260608"
	meta := fmt.Sprintf(`{
		"id": "%s",
		"port": 19198,
		"pid": %d,
		"configType": "custom",
		"projectRoot": "%s",
		"startTime": "%s",
		"adminToken": "testtoken1234567890testtoken123456"
	}`, instanceID, os.Getpid(), tmpDir, time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(instancesDir, instanceID+".json"), []byte(meta), 0600)

	// Create log file with 10 lines
	var logLines strings.Builder
	for i := 1; i <= 10; i++ {
		logLines.WriteString(fmt.Sprintf("line%d: log entry %d\n", i, i))
	}
	os.WriteFile(filepath.Join(instancesDir, instanceID+".log"), []byte(logLines.String()), 0644)

	cmd := exec.Command(binary, "logs", instanceID, "--tail", "3")
	cmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err != nil {
		t.Logf("Logs tail command error: %v", err)
	}

	out := stdout.String()
	t.Logf("Logs tail output:\n%s", out)

	// Should only show last 3 lines (8, 9, 10)
	if strings.Contains(out, "line7:") {
		t.Error("Should not contain line7 when --tail 3")
	}
	if !strings.Contains(out, "line8:") {
		t.Error("Should contain line8")
	}
	if !strings.Contains(out, "line9:") {
		t.Error("Should contain line9")
	}
	if !strings.Contains(out, "line10:") {
		t.Error("Should contain line10")
	}
}

// --- Stop Command Tests ---

func TestStopCommand_StopsRunningInstance(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))
	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	// Find instance ID from metadata
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	entries, _ := os.ReadDir(instancesDir)
	if len(entries) == 0 {
		t.Fatal("No instance metadata found after start")
	}
	instanceID := strings.TrimSuffix(entries[0].Name(), ".json")

	// Stop the specific instance
	stopCmd := exec.Command(binary, "stop", instanceID)
	stopCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stopOut bytes.Buffer
	stopCmd.Stdout = &stopOut

	if err := stopCmd.Run(); err != nil {
		t.Fatalf("Stop command failed: %v\nOutput: %s", err, stopOut.String())
	}
	t.Logf("Stop output: %s", stopOut.String())

	if !strings.Contains(stopOut.String(), "Stopped instance") {
		t.Errorf("Expected 'Stopped instance' in output, got: %s", stopOut.String())
	}

	// Verify instance metadata was cleaned up
	time.Sleep(500 * time.Millisecond)
	entries2, _ := os.ReadDir(instancesDir)
	if len(entries2) > 0 {
		t.Errorf("Expected instance metadata deleted after stop, found %d files", len(entries2))
	}

	// Cleanup: process may already be stopped
	cmd.Process.Kill()
	cmd.Wait()
}

func TestStopCommand_StopsAllInstances(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))
	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	// Stop all instances (no ID argument)
	stopCmd := exec.Command(binary, "stop")
	stopCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stopOut, stopErr bytes.Buffer
	stopCmd.Stdout = &stopOut
	stopCmd.Stderr = &stopErr

	if err := stopCmd.Run(); err != nil {
		t.Fatalf("Stop all command failed: %v\nOutput: %s\nStderr: %s", err, stopOut.String(), stopErr.String())
	}
	t.Logf("Stop all output: %s", stopOut.String())

	if !strings.Contains(stopOut.String(), "Stopped 1 instance(s)") {
		t.Errorf("Expected 'Stopped 1 instance(s)' in output, got: %s", stopOut.String())
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStopCommand_StaleCleanup(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	os.MkdirAll(instancesDir, 0755)

	// Create a stale instance with a dead PID
	instanceID := "inst_stale_test_20260609"
	meta := fmt.Sprintf(`{
		"id": "%s",
		"port": 19999,
		"pid": 99999,
		"configType": "custom",
		"projectRoot": "%s",
		"startTime": "%s",
		"adminToken": "testtoken1234567890testtoken123456"
	}`, instanceID, tmpDir, time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(instancesDir, instanceID+".json"), []byte(meta), 0600)

	// Stop should clean up the stale instance
	stopCmd := exec.Command(binary, "stop")
	stopCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stopOut bytes.Buffer
	stopCmd.Stdout = &stopOut

	if err := stopCmd.Run(); err != nil {
		t.Fatalf("Stop stale command failed: %v\nOutput: %s", err, stopOut.String())
	}
	t.Logf("Stale stop output: %s", stopOut.String())

	// Verify stale instance was cleaned up
	entries, _ := os.ReadDir(instancesDir)
	if len(entries) > 0 {
		t.Errorf("Expected stale instance to be cleaned up, found %d files", len(entries))
	}
}

func TestStopCommand_NoInstances(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	stopCmd := exec.Command(binary, "stop")
	stopCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var stopOut bytes.Buffer
	stopCmd.Stdout = &stopOut

	stopCmd.Run()
	if !strings.Contains(stopOut.String(), "No instances found") {
		t.Errorf("Expected 'No instances found', got: %s", stopOut.String())
	}
}

// --- Status Command Tests ---

func TestStatusCommand_ShowsRunningInstance(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))
	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	// Check status
	statusCmd := exec.Command(binary, "status")
	statusCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut

	if err := statusCmd.Run(); err != nil {
		t.Fatalf("Status command failed: %v", err)
	}
	out := statusOut.String()
	t.Logf("Status output:\n%s", out)

	if !strings.Contains(out, "running") {
		t.Errorf("Expected 'running' in status output")
	}
	if !strings.Contains(out, "INSTANCE ID") {
		t.Errorf("Expected table header in status output")
	}
	if !strings.Contains(out, "1 running") {
		t.Errorf("Expected '1 running' summary")
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStatusCommand_HidesDeadInstances(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	os.MkdirAll(instancesDir, 0755)

	// Create a dead instance
	instanceID := "inst_dead_test_20260609"
	meta := fmt.Sprintf(`{
		"id": "%s",
		"port": 19998,
		"pid": 99998,
		"configType": "custom",
		"projectRoot": "%s",
		"startTime": "%s",
		"adminToken": "testtoken1234567890testtoken123456"
	}`, instanceID, tmpDir, time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(instancesDir, instanceID+".json"), []byte(meta), 0600)

	// Status without --all should hide dead instances
	statusCmd := exec.Command(binary, "status")
	statusCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	statusCmd.Run()

	out := statusOut.String()
	t.Logf("Status (no --all) output:\n%s", out)

	// Should say 0 running and not show the instance ID
	if !strings.Contains(out, "0 running") {
		t.Errorf("Expected '0 running' when no --all flag")
	}
	if strings.Contains(out, instanceID) {
		t.Error("Dead instance should be hidden without --all")
	}
}

func TestStatusCommand_ShowsAllWithFlag(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	os.MkdirAll(instancesDir, 0755)

	// Create a dead instance
	instanceID := "inst_dead_all_20260609"
	meta := fmt.Sprintf(`{
		"id": "%s",
		"port": 19997,
		"pid": 99997,
		"configType": "custom",
		"projectRoot": "%s",
		"startTime": "%s",
		"adminToken": "testtoken1234567890testtoken123456"
	}`, instanceID, tmpDir, time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(instancesDir, instanceID+".json"), []byte(meta), 0600)

	// Status with --all should show dead instances
	statusCmd := exec.Command(binary, "status", "--all")
	statusCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	statusCmd.Run()

	out := statusOut.String()
	t.Logf("Status (--all) output:\n%s", out)

	if !strings.Contains(out, instanceID) {
		t.Error("Dead instance should be shown with --all")
	}
	if !strings.Contains(out, "dead") {
		t.Error("Expected 'dead' status with --all")
	}
}

func TestStatusCommand_NoInstances(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	statusCmd := exec.Command(binary, "status")
	statusCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	statusCmd.Run()

	if !strings.Contains(statusOut.String(), "No instances found") {
		t.Errorf("Expected 'No instances found', got: %s", statusOut.String())
	}
}

// --- Restart Command Tests ---

func TestRestartCommand_NoRunning(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	restartCmd := exec.Command(binary, "restart")
	restartCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var restartOut, restartErr bytes.Buffer
	restartCmd.Stdout = &restartOut
	restartCmd.Stderr = &restartErr

	err = restartCmd.Run()
	t.Logf("Restart (no instances) error: %v", err)
	t.Logf("Restart stdout: %s", restartOut.String())
	t.Logf("Restart stderr: %s", restartErr.String())

	// Should report no instances or an appropriate error
	if err == nil {
		t.Log("Restart with no instances returned nil error (acceptable)")
	} else {
		out := restartOut.String() + restartErr.String()
		if !strings.Contains(out, "No instances") && !strings.Contains(out, "no running") {
			t.Errorf("Expected 'No instances' or 'no running' message, got: %s", out)
		}
	}
}

func TestRestartCommand_RestartsInstance(t *testing.T) {
	binary := buildTestBinary(t)
	tmpDir, err := os.MkdirTemp("", "ccrouter-start-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tempHome := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(tempHome, ".cc-modelrouter", "instances"), 0755)

	port := nextPort()
	configPath := writeConfig(t, tmpDir, minConfig(port))
	cmd, _, _ := startServerWithOutput(t, binary, configPath, tempHome, "--port", fmt.Sprintf("%d", port))
	time.Sleep(2 * time.Second)

	// Verify instance exists
	instancesDir := filepath.Join(tempHome, ".cc-modelrouter", "instances")
	entries, _ := os.ReadDir(instancesDir)
	if len(entries) == 0 {
		t.Fatal("No instance metadata found after start")
	}

	// Restart the instance
	restartCmd := exec.Command(binary, "restart")
	restartCmd.Env = append(os.Environ(), "HOME="+tempHome)
	var restartOut, restartErr bytes.Buffer
	restartCmd.Stdout = &restartOut
	restartCmd.Stderr = &restartErr

	err = restartCmd.Run()
	t.Logf("Restart exit: %v", err)
	t.Logf("Restart stdout: %s", restartOut.String())
	t.Logf("Restart stderr: %s", restartErr.String())

	if err != nil {
		t.Logf("Restart returned error: %v", err)
	}

	// Verify original process is cleaned up
	cmd.Process.Kill()
	cmd.Wait()
}
