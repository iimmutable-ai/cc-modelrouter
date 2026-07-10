package cli

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
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

// TestBuildConfig_NilExisting_PresetProvider verifies the first-install path:
// when no existing config is supplied, preset providers get their preset
// baseURL, the env-var placeholder for the API key, and preset models.
func TestBuildConfig_NilExisting_PresetProvider(t *testing.T) {
	ans := &collectedAnswers{
		Host:             "0.0.0.0",
		Port:             "8443",
		TLSMode:          "none",
		Scope:            svcinstall.ScopeSystem,
		APIKeys:          map[string]string{"bigmodel": "sk-synthetic-key"},
		TestModels:       map[string]string{"bigmodel": "glm-4.6"},
		ProviderBaseURLs: map[string]string{},
	}

	cfg := buildConfig(ans, nil)

	pc, ok := cfg.Providers["bigmodel"]
	if !ok {
		t.Fatalf("expected providers[bigmodel] to exist")
	}
	if pc.APIKey != "${CCROUTER_BIGMODEL_API_KEY}" {
		t.Errorf("APIKey = %q, want placeholder", pc.APIKey)
	}
	if pc.BaseURL == "" {
		t.Errorf("BaseURL should be populated from preset for preset providers; got empty")
	}
	if len(pc.Models) == 0 {
		t.Errorf("Models should be populated from preset for preset providers; got empty")
	}
}

// TestBuildConfig_PreservesCustomProvider is the regression test for the
// original bug. An existing config.json from `ccrouter config` had a custom
// provider "myglm" with a real baseURL and transformer. setup server was
// rebuilding providers from scratch and zeroing those fields, causing
// `ccrouter start` to fail with "baseURL is required". The fix: merge onto
// the existing config so those fields survive.
func TestBuildConfig_PreservesCustomProvider(t *testing.T) {
	existing := config.Defaults()
	existing.Providers["myglm"] = config.ProviderConfig{
		APIKey:      "${CCROUTER_MYGLM_API_KEY}",
		BaseURL:     "https://api.myglm.example.com",
		Transformer: "anthropic",
		Models:      []string{"my-model-1", "my-model-2"},
	}

	ans := &collectedAnswers{
		Host:             "0.0.0.0",
		Port:             "8443",
		TLSMode:          "none",
		Scope:            svcinstall.ScopeSystem,
		APIKeys:          map[string]string{"myglm": "sk-synthetic-newkey"},
		TestModels:       map[string]string{"myglm": "my-model-1"},
		ProviderBaseURLs: map[string]string{},
	}

	cfg := buildConfig(ans, existing)

	pc, ok := cfg.Providers["myglm"]
	if !ok {
		t.Fatalf("providers[myglm] missing")
	}
	if pc.BaseURL != "https://api.myglm.example.com" {
		t.Errorf("BaseURL = %q; want preserved value", pc.BaseURL)
	}
	if pc.Transformer != "anthropic" {
		t.Errorf("Transformer = %q; want preserved value", pc.Transformer)
	}
	if len(pc.Models) != 2 || pc.Models[0] != "my-model-1" || pc.Models[1] != "my-model-2" {
		t.Errorf("Models = %v; want preserved [my-model-1 my-model-2]", pc.Models)
	}
	if pc.APIKey != "${CCROUTER_MYGLM_API_KEY}" {
		t.Errorf("APIKey = %q; want placeholder (refreshed)", pc.APIKey)
	}
}

// TestBuildConfig_PreservesRoutes verifies that a hand-edited default route
// (e.g. one configured via `ccrouter config`) is NOT replaced when setup
// re-runs. setup only synthesizes a default route when none exists.
func TestBuildConfig_PreservesRoutes(t *testing.T) {
	existing := config.Defaults()
	existing.Providers["bigmodel"] = config.ProviderConfig{
		APIKey:      "${CCROUTER_BIGMODEL_API_KEY}",
		BaseURL:     "https://open.bigmodel.cn/api",
		Transformer: "glm",
		Models:      []string{"glm-4.6"},
	}
	existing.Router.Routes["default"] = "bigmodel:glm-4.6;openrouter:anthropic/claude-sonnet-4"

	ans := &collectedAnswers{
		Host:             "0.0.0.0",
		Port:             "8443",
		TLSMode:          "none",
		Scope:            svcinstall.ScopeSystem,
		APIKeys:          map[string]string{"bigmodel": "sk-synthetic"},
		TestModels:       map[string]string{"bigmodel": "glm-4.6"},
		ProviderBaseURLs: map[string]string{},
	}

	cfg := buildConfig(ans, existing)

	got := cfg.Router.Routes["default"]
	want := "bigmodel:glm-4.6;openrouter:anthropic/claude-sonnet-4"
	if got != want {
		t.Errorf("default route = %q; want preserved %q", got, want)
	}
}

// TestBuildConfig_PreservesAutoRestart verifies that a user-tuned restart
// window survives re-running setup. The plan only fills defaults when the
// existing config's fields are empty.
func TestBuildConfig_PreservesAutoRestart(t *testing.T) {
	existing := config.Defaults()
	existing.Server.AutoRestartIdle = "4h"
	existing.Server.AutoRestartBackoffMax = "30m"

	ans := &collectedAnswers{
		Host:             "0.0.0.0",
		Port:             "8443",
		TLSMode:          "none",
		Scope:            svcinstall.ScopeSystem,
		APIKeys:          map[string]string{},
		TestModels:       map[string]string{},
		ProviderBaseURLs: map[string]string{},
	}

	cfg := buildConfig(ans, existing)

	if cfg.Server.AutoRestartIdle != "4h" {
		t.Errorf("AutoRestartIdle = %q; want preserved 4h", cfg.Server.AutoRestartIdle)
	}
	if cfg.Server.AutoRestartBackoffMax != "30m" {
		t.Errorf("AutoRestartBackoffMax = %q; want preserved 30m", cfg.Server.AutoRestartBackoffMax)
	}
}

// TestBuildConfig_NewCustomProvider_UsesProviderBaseURLs covers the case
// where the operator adds a brand-new custom provider at [4/5] (no preset,
// no existing entry). The operator-typed baseURL must land on the new
// provider entry, and transformer defaults to "anthropic" so the entry is
// immediately usable.
func TestBuildConfig_NewCustomProvider_UsesProviderBaseURLs(t *testing.T) {
	ans := &collectedAnswers{
		Host:       "0.0.0.0",
		Port:       "8443",
		TLSMode:    "none",
		Scope:      svcinstall.ScopeSystem,
		APIKeys:    map[string]string{"acme": "sk-synthetic-acme"},
		TestModels: map[string]string{"acme": "acme-1"},
		ProviderBaseURLs: map[string]string{
			"acme": "https://api.acme.example.com/v1",
		},
	}

	cfg := buildConfig(ans, nil)

	pc, ok := cfg.Providers["acme"]
	if !ok {
		t.Fatalf("providers[acme] missing")
	}
	if pc.BaseURL != "https://api.acme.example.com/v1" {
		t.Errorf("BaseURL = %q; want operator-entered URL", pc.BaseURL)
	}
	if pc.Transformer != "anthropic" {
		t.Errorf("Transformer = %q; want default \"anthropic\" for new custom providers", pc.Transformer)
	}
	if pc.APIKey != "${CCROUTER_ACME_API_KEY}" {
		t.Errorf("APIKey = %q; want placeholder", pc.APIKey)
	}
}

// TestValidateAutocertDomain exercises the autocert FQDN validator used by
// the [2/5] TLS prompt loop. The validator exists because two failure modes
// can't be detected from config alone but will definitely break the service
// at start time: an empty domain (autocert Manager has nothing to mint) and
// an IP literal (Let's Encrypt won't issue via http-01 for IP identifiers).
func TestValidateAutocertDomain(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		wantOK bool
	}{
		{"empty", "", false},
		{"IPv4 literal", "43.108.32.178", false},
		{"IPv6 literal", "2001:db8::1", false},
		{"IPv6 loopback", "::1", false},
		{"valid DNS name", "ccrouter.example.com", true},
		{"valid DNS single label", "localhost", true},
		{"valid DNS with trailing dot", "ccrouter.example.com.", true},
		{"mixed case", "CCRouter.Example.COM", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := validateAutocertDomain(tt.domain)
			gotOK := reason == ""
			if gotOK != tt.wantOK {
				t.Errorf("validateAutocertDomain(%q) ok=%v, want %v (reason=%q)",
					tt.domain, gotOK, tt.wantOK, reason)
			}
		})
	}
}

// TestValidateAutocertDomain_RejectsIPWithReason verifies the IP-rejection
// path returns a message that mentions the IP and points the user at the
// alternative (manual TLS mode), so the prompt loop can surface it.
func TestValidateAutocertDomain_RejectsIPWithReason(t *testing.T) {
	reason := validateAutocertDomain("43.108.32.178")
	if reason == "" {
		t.Fatalf("expected rejection for IP literal, got OK")
	}
	if !strings.Contains(reason, "43.108.32.178") {
		t.Errorf("rejection reason should mention the IP; got: %q", reason)
	}
	if !strings.Contains(reason, "manual") {
		t.Errorf("rejection reason should suggest manual TLS mode; got: %q", reason)
	}
}

// TestValidateScopeTLSCombo table-drives the scope+TLS cross-validation
// added for Bug 1. The only failing combo is user-scope + autocert (service
// can't bind :80 for the ACME http-01 challenge and would restart-loop);
// every other combination is accepted so setup proceeds normally.
func TestValidateScopeTLSCombo(t *testing.T) {
	tests := []struct {
		name    string
		scope   svcinstall.Scope
		tlsMode string
		wantOK  bool
	}{
		{"user + autocert (bug case)", svcinstall.ScopeUser, "autocert", false},
		{"user + manual", svcinstall.ScopeUser, "manual", true},
		{"user + none", svcinstall.ScopeUser, "none", true},
		{"system + autocert", svcinstall.ScopeSystem, "autocert", true},
		{"system + manual", svcinstall.ScopeSystem, "manual", true},
		{"system + none", svcinstall.ScopeSystem, "none", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ans := &collectedAnswers{Scope: tt.scope, TLSMode: tt.tlsMode}
			reason := validateScopeTLSCombo(ans)
			gotOK := reason == ""
			if gotOK != tt.wantOK {
				t.Errorf("validateScopeTLSCombo(scope=%v, tls=%q) ok=%v, want %v (reason=%q)",
					tt.scope, tt.tlsMode, gotOK, tt.wantOK, reason)
			}
		})
	}
}

// TestValidateScopeTLSCombo_RejectsWithActionableMessage verifies the
// user-scope+autocert rejection message points the operator at both
// remediation paths (system-scope, manual TLS) so the prompt loop's
// re-pick guidance is actionable rather than just "no".
func TestValidateScopeTLSCombo_RejectsWithActionableMessage(t *testing.T) {
	ans := &collectedAnswers{Scope: svcinstall.ScopeUser, TLSMode: "autocert"}
	reason := validateScopeTLSCombo(ans)
	if reason == "" {
		t.Fatalf("expected rejection for user+autocert, got OK")
	}
	if !strings.Contains(reason, ":80") {
		t.Errorf("rejection should explain the :80 binding requirement; got: %q", reason)
	}
	if !strings.Contains(reason, "system-scope") {
		t.Errorf("rejection should suggest system-scope; got: %q", reason)
	}
	if !strings.Contains(reason, "Manual") {
		t.Errorf("rejection should suggest manual TLS mode; got: %q", reason)
	}
}
