package cli

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/svcinstall"
)

func TestWarnAboutBinaryLocation(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	homeBin := filepath.Join(u.HomeDir, "ccrouter")
	homeSubBin := filepath.Join(u.HomeDir, "go", "bin", "ccrouter")

	tests := []struct {
		name    string
		binPath string
		want    bool
	}{
		{"in tmp", "/tmp/ccrouter", true},
		{"in tmp subdir", "/tmp/build/ccrouter", true},
		{"in home", homeBin, true},
		{"in home subdir", homeSubBin, true},
		{"in usr local bin", "/usr/local/bin/ccrouter", false},
		{"in usr bin", "/usr/bin/ccrouter", false},
		{"in opt", "/opt/ccrouter/bin/ccrouter", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := warnAboutBinaryLocation(tt.binPath)
			if got != tt.want {
				t.Errorf("warnAboutBinaryLocation(%q) = %v, want %v", tt.binPath, got, tt.want)
			}
		})
	}
}

func TestSortedProviderNames(t *testing.T) {
	got := sortedProviderNames(nil)
	if len(got) != 0 {
		t.Errorf("sortedProviderNames(nil) = %v, want empty", got)
	}
}

func TestNewSetupServerCommand(t *testing.T) {
	cmd := NewSetupServerCommand()

	if cmd.Use != "server" {
		t.Errorf("expected Use %q, got %q", "server", cmd.Use)
	}

	f := cmd.Flags().Lookup("dry-run")
	if f == nil {
		t.Fatal("expected --dry-run flag")
	}
	if f.DefValue != "false" {
		t.Errorf("expected --dry-run default %q, got %q", "false", f.DefValue)
	}

	f2 := cmd.Flags().Lookup("config")
	if f2 == nil {
		t.Fatal("expected --config flag")
	}
}

// TestWriteEnvFileForSystemd_UserScope_WritesDirectly exercises the user-scope
// path of writeEnvFileForSystemd: the file must be written without sudo, with
// mode 0600, and in raw KEY=value format (no `export` prefix, no shell quotes).
// This is the format systemd's EnvironmentFile= parser requires; shell_env.sh's
// `export FOO="bar"` format gets silently dropped, leaving API key placeholders
// unexpanded and causing start.go:313-318 to exit 1.
func TestWriteEnvFileForSystemd_UserScope_WritesDirectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cc-modelrouter", "service.env")

	apiKeys := map[string]string{
		"bigmodel":   "sk-synthetic-bm-abc123",
		"openrouter": "sk-or-synthetic-xyz789",
		// Empty keys must be skipped, not written as `VAR=`.
		"gemini": "",
	}

	if err := writeEnvFileForSystemd(path, apiKeys, svcinstall.ScopeUser); err != nil {
		t.Fatalf("writeEnvFileForSystemd user scope failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat service.env: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read service.env: %v", err)
	}
	body := string(data)
	if strings.HasPrefix(strings.TrimSpace(body), "export ") {
		t.Errorf("service.env must not use `export` prefix (systemd drops it); got:\n%s", body)
	}
	if strings.Contains(body, `"`) {
		t.Errorf("service.env must not contain shell quotes; got:\n%s", body)
	}
	if strings.Contains(body, "CCROUTER_GEMINI_API_KEY=") {
		t.Errorf("empty key should be skipped, not written; got:\n%s", body)
	}
	for _, want := range []string{
		"CCROUTER_BIGMODEL_API_KEY=sk-synthetic-bm-abc123",
		"CCROUTER_OPENROUTER_API_KEY=sk-or-synthetic-xyz789",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service.env missing %q; got:\n%s", want, body)
		}
	}
}

// TestWriteEnvFileForSystemd_UserScope_KeyFormat parses the written file back
// and asserts every line matches the strict `^[A-Z_][A-Z0-9_]*=value$` form
// systemd's EnvironmentFile= parser accepts. A regression that reintroduces
// quotes or `export` would fail here.
func TestWriteEnvFileForSystemd_UserScope_KeyFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.env")
	apiKeys := map[string]string{
		"openrouter": "sk-or-synthetic-1",
		"bigmodel":   "sk-synthetic-2",
		"anthropic":  "sk-ant-synthetic-3",
	}
	if err := writeEnvFileForSystemd(path, apiKeys, svcinstall.ScopeUser); err != nil {
		t.Fatalf("writeEnvFileForSystemd user scope failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read service.env: %v", err)
	}

	got := map[string]string{}
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		eq := strings.Index(line, "=")
		if eq <= 0 {
			t.Errorf("line %d: missing KEY=value form: %q", i+1, line)
			continue
		}
		key, val := line[:eq], line[eq+1:]
		if !strings.HasPrefix(key, "CCROUTER_") || !strings.HasSuffix(key, "_API_KEY") {
			t.Errorf("line %d: unexpected env var name %q", i+1, key)
			continue
		}
		got[key] = val
	}

	want := map[string]string{
		"CCROUTER_OPENROUTER_API_KEY": "sk-or-synthetic-1",
		"CCROUTER_BIGMODEL_API_KEY":   "sk-synthetic-2",
		"CCROUTER_ANTHROPIC_API_KEY":  "sk-ant-synthetic-3",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// TestWriteEnvFileForSystemd_SystemScope_UsesSudo verifies the system-scope
// branch routes through sudo. On systems without sudo (typical CI), we skip
// rather than fail — the user-scope tests already cover the format invariants.
func TestWriteEnvFileForSystemd_SystemScope_UsesSudo(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("system-scope service.env writes are linux-only")
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skip("sudo not available in test environment")
	}

	// Write to a path under /tmp that requires sudo: /tmp itself is
	// world-writable, but the synthetic /etc/cc-modelrouter path isn't.
	// We use a /tmp subpath to keep the test hermetic while still exercising
	// the sudo code path (sudo mkdir + sudo tee + sudo chmod).
	dir := t.TempDir()
	rootOwned := filepath.Join(dir, "needs-root")
	path := filepath.Join(rootOwned, "service.env")

	// Pre-create the parent dir root-owned so the inner mkdir has to escalate.
	if out, err := exec.Command("sudo", "mkdir", "-p", rootOwned).CombinedOutput(); err != nil {
		t.Fatalf("sudo mkdir parent: %v (%s)", err, string(out))
	}
	if out, err := exec.Command("sudo", "chmod", "0755", rootOwned).CombinedOutput(); err != nil {
		t.Fatalf("sudo chmod parent: %v (%s)", err, string(out))
	}

	apiKeys := map[string]string{"bigmodel": "sk-synthetic-system-1"}
	if err := writeEnvFileForSystemd(path, apiKeys, svcinstall.ScopeSystem); err != nil {
		t.Fatalf("writeEnvFileForSystemd system scope failed: %v", err)
	}

	// Verify the file landed at the target path with the right content. Reading
	// a root-owned file (mode 0640, group ccrouter may not exist) requires sudo.
	out, err := exec.Command("sudo", "cat", path).CombinedOutput()
	if err != nil {
		t.Fatalf("sudo cat: %v (%s)", err, string(out))
	}
	if !strings.Contains(string(out), "CCROUTER_BIGMODEL_API_KEY=sk-synthetic-system-1") {
		t.Errorf("unexpected content: %s", string(out))
	}

	// Cleanup root-owned artifacts.
	_, _ = exec.Command("sudo", "rm", "-rf", rootOwned).CombinedOutput()
}
