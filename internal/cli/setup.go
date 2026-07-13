// Package cli — setup command group.
//
// `ccrouter setup server` is a non-TUI installer for running ccrouter as
// a public-facing HTTPS service on a Linux server (Alibaba Cloud Linux,
// Ubuntu, Debian, CentOS, etc.). It walks the operator through bind
// address, TLS mode, service level, and provider API keys; validates
// each key by sending a 1-token test request; writes config.json with
// ${VAR} placeholders; persists real keys to ~/.cc-modelrouter/shell_env.sh
// (and, for system-scope installs, to /etc/cc-modelrouter/service.env);
// and installs a systemd unit.
package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/configwizard"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/setupprompt"
	"github.com/iimmutable-ai/cc-modelrouter/internal/svcinstall"
	"github.com/spf13/cobra"
)

// NewSetupCommand is the parent for the setup subgroup. It has no RunE
// because there's nothing to do at the group level — the leaves do the
// work.
func NewSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Guided setup commands",
		Long:  `Guided setup commands for specific deployment scenarios. Currently supports "server" for installing ccrouter as a public-facing HTTPS service on Linux.`,
	}
	cmd.AddCommand(NewSetupServerCommand())
	return cmd
}

// NewSetupServerCommand implements `ccrouter setup server`.
//
// Flags:
//
//	--dry-run  Walk through all prompts, print config + unit body to stdout,
//	           write nothing, install nothing.
//	--config   Override the config file path (defaults to global config).
func NewSetupServerCommand() *cobra.Command {
	var dryRun bool
	var configPath string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Set up ccrouter as a public-facing HTTPS service",
		Long: `Walk through bind address, TLS, service level, and provider API keys, then
install ccrouter as a systemd service. Validates each API key with a 1-token
test request before saving.

Secrets handling:
  - Real API keys are written to ~/.cc-modelrouter/shell_env.sh (mode 0600).
  - For system-level installs, keys are also written to
    /etc/cc-modelrouter/service.env (root:ccrouter, mode 0640) so the
    unprivileged service user can read them.
  - config.json stores only ${CCROUTER_<NAME>_API_KEY} placeholders.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupServer(dryRun, configPath)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print config + unit body; write nothing")
	cmd.Flags().StringVar(&configPath, "config", "", "Override config file path (default: global)")
	return cmd
}

// collectedAnswers holds everything the Q&A extracted from the user.
type collectedAnswers struct {
	Host       string
	Port       string
	TLSMode    string // "autocert" | "manual" | "none"
	Domain     string
	CertFile   string
	KeyFile    string
	Redirect   bool
	Scope      svcinstall.Scope
	APIKeys    map[string]string // provider name → real key
	TestModels map[string]string // provider name → model used for TestProviderConnection
	// ProviderBaseURLs captures operator-entered baseURLs for custom
	// (non-preset) providers added at [4/5]. Only populated for providers
	// that had no entry in the existing config.json at setup time —
	// preset providers and preserved existing entries already have one.
	ProviderBaseURLs map[string]string
}

func runSetupServer(dryRun bool, configPathOverride string) error {
	p := setupprompt.New()

	fmt.Println("\n=== ccrouter server setup ===")
	if dryRun {
		fmt.Println("(dry-run mode: nothing will be written)")
	}

	ans, err := collectAnswers(p)
	if err != nil {
		return err
	}

	if !confirmAndReview(p, ans) {
		fmt.Println("Aborted; nothing was written.")
		return nil
	}

	return applyAnswers(ans, dryRun, configPathOverride)
}

// collectAnswers runs the 5-step Q&A.
func collectAnswers(p *setupprompt.Prompt) (*collectedAnswers, error) {
	ans := &collectedAnswers{
		APIKeys:          map[string]string{},
		TestModels:       map[string]string{},
		ProviderBaseURLs: map[string]string{},
	}

	// [1/5] Bind address
	fmt.Println("\n[1/5] Bind address")
	ans.Host = p.AskString("  Host", "0.0.0.0")
	for {
		port := p.AskString("  Port", "8443")
		if configwizard.ValidatePort(port) {
			ans.Port = port
			break
		}
		fmt.Printf("  Invalid port %q (must be 1024-65535); try again.\n", port)
	}

	// [2/5] TLS
	fmt.Println("\n[2/5] HTTPS / TLS")
	tlsIdx := p.AskChoice("  Select TLS mode:", []string{
		"Let's Encrypt (autocert) — requires a domain and open :80/:443",
		"Manual cert/key file paths",
		"Plain HTTP (NOT recommended for public servers)",
	}, 0)
	switch tlsIdx {
	case 0:
		ans.TLSMode = "autocert"
		ans.Domain = strings.TrimSpace(p.AskString("  Domain (FQDN)", ""))
		for {
			if reason := validateAutocertDomain(ans.Domain); reason != "" {
				fmt.Printf("  %s\n", reason)
				ans.Domain = strings.TrimSpace(p.AskString("  Domain (FQDN)", ""))
				continue
			}
			break
		}
		ans.Redirect = p.AskYesNo("  Redirect HTTP→HTTPS? (also serves the ACME http-01 challenge)", true)
	case 1:
		ans.TLSMode = "manual"
		ans.CertFile = p.AskString("  Cert file (PEM)", "")
		ans.KeyFile = p.AskString("  Key file (PEM)", "")
		ans.Redirect = p.AskYesNo("  Redirect HTTP→HTTPS on :80?", true)
	case 2:
		ans.TLSMode = "none"
		if !p.AskYesNo("  Plain HTTP on a public server is risky. Continue anyway?", false) {
			return nil, fmt.Errorf("user aborted at HTTP confirmation")
		}
	}

	// [3/5] Service level
	fmt.Println("\n[3/5] Service install level")
	scopeIdx := p.AskChoice("  Select service level:", []string{
		"System-level — dedicated `ccrouter` user, sudo required (recommended)",
		"User-level — runs as current user, no sudo",
	}, 0)
	ans.Scope = svcinstall.Scope(scopeIdx)

	// Cross-validate TLS + scope: user-scope + autocert cannot bind :80
	// for the ACME http-01 challenge (no CAP_NET_BIND_SERVICE), so the
	// service would start, fail to bind, and restart-loop forever. Loop
	// so the user can re-pick scope after reading why.
	for {
		if reason := validateScopeTLSCombo(ans); reason != "" {
			fmt.Println("\n  ✗ " + reason)
			fmt.Println("  Re-pick service level to continue.")
			scopeIdx = p.AskChoice("  Select service level:", []string{
				"System-level — dedicated `ccrouter` user, sudo required (recommended)",
				"User-level — runs as current user, no sudo",
			}, 0)
			ans.Scope = svcinstall.Scope(scopeIdx)
			continue
		}
		break
	}

	// [4/5] Provider API keys
	fmt.Println("\n[4/5] Provider API keys")
	fmt.Println("  Known providers: openrouter, bigmodel, gemini, anthropic, openai, aliyun, minimax")
	fmt.Println("  (You can also type any custom name.)")
	collectProviders(p, ans)

	// [5/5] Review happens in confirmAndReview before any write.
	return ans, nil
}

// collectProviders loops on provider entry + key validation until the
// user submits an empty provider name. Keys are validated by sending a
// real 1-token request via TestProviderConnection; failures must re-prompt.
//
// On EOF (Ctrl-D or end of piped input), the loop aborts immediately so
// we don't spin forever on empty input.
func collectProviders(p *setupprompt.Prompt, ans *collectedAnswers) {
	// Load the existing config.json (if any) so we can reuse a previously
	// set baseURL for a custom-named provider instead of re-prompting.
	// A missing file just means existingProviders stays nil.
	var existingProviders map[string]config.ProviderConfig
	if cfgLoaded, err := config.Load(config.GlobalConfigPath()); err == nil {
		existingProviders = cfgLoaded.Providers
	}

	// Discover existing keys so we don't clobber them on skip.
	if shellCfg, err := configwizard.GetShellConfig(); err == nil {
		if existing, err := shellCfg.LoadEnvFile(); err == nil && len(existing) > 0 {
			names := make([]string, 0, len(existing))
			for n := range existing {
				names = append(names, n)
			}
			sort.Strings(names)
			fmt.Printf("  Found %d existing provider(s) in ~/.cc-modelrouter/shell_env.sh: %s\n",
				len(existing), strings.Join(names, ", "))
			if p.AskYesNo("  Use these providers for this install?", true) {
				for _, name := range names {
					preset, hasPreset := configwizard.ProviderPresets[name]
					testModel := ""
					if hasPreset && len(preset.Models) > 0 {
						testModel = preset.Models[0]
					}
					ans.APIKeys[name] = existing[name]
					ans.TestModels[name] = testModel
				}
				fmt.Printf("  Loaded %d provider(s).\n", len(ans.APIKeys))
				if !p.AskYesNo("  Add another provider?", false) {
					return
				}
			}
		}
	}
	for {
		if p.EOF() {
			return
		}
		name := strings.TrimSpace(p.AskString("\n  Provider name (blank=done)", ""))
		if p.EOF() {
			return
		}
		if name == "" {
			if len(ans.APIKeys) == 0 {
				if !p.AskYesNo("  No providers configured — the router will not be usable. Continue anyway?", false) {
					continue
				}
			}
			return
		}
		preset, hasPreset := configwizard.ProviderPresets[name]
		var testModel string
		if hasPreset && len(preset.Models) > 0 {
			testModel = preset.Models[0]
			fmt.Printf("  Using preset: %s, default test model %q\n", preset.BaseURL, testModel)
		}
		// For non-preset providers we need a baseURL to build a working
		// config entry. Try the existing config first (preserves a URL the
		// operator already set via `ccrouter config`); otherwise prompt.
		var baseURL string
		if hasPreset {
			baseURL = preset.BaseURL
		} else if existingProviders != nil {
			if pc, ok := existingProviders[name]; ok && pc.BaseURL != "" {
				baseURL = pc.BaseURL
				fmt.Printf("  Reusing baseURL from existing config: %s\n", baseURL)
			}
		}
		if baseURL == "" {
			baseURL = strings.TrimSpace(p.AskString("  Base URL (e.g. https://api.example.com/v1)", ""))
			for baseURL == "" {
				fmt.Println("  Base URL is required when there's no preset for this provider.")
				baseURL = strings.TrimSpace(p.AskString("  Base URL", ""))
			}
		}
		testModel = p.AskString("  Model to test with", testModel)
		if testModel == "" {
			fmt.Println("  A test model is required to validate the key.")
			continue
		}

		key := p.AskPassword(fmt.Sprintf("  API key for %s", name))
		if key == "" {
			fmt.Println("  Empty key; skipping this provider.")
			continue
		}

		// Build a temporary ProviderConfig with the literal key for the
		// connectivity test. The literal never touches disk.
		testCfg := config.ProviderConfig{
			APIKey:      key,
			BaseURL:     baseURL,
			Transformer: preset.Transformer,
		}
		fmt.Printf("  → testing connection to %s...\n", name)
		result := configwizard.TestProviderConnection(name, testCfg, testModel)
		if !result.Success {
			fmt.Printf("  ✗ connection test failed: %s\n", result.Error)
			if p.AskYesNo("  Save this key anyway? (NOT recommended)", false) {
				ans.APIKeys[name] = key
				ans.TestModels[name] = testModel
				if !hasPreset {
					ans.ProviderBaseURLs[name] = baseURL
				}
			}
			continue
		}
		fmt.Printf("  ✓ OK (%dms, in=%d out=%d tokens)\n",
			result.Latency.Milliseconds(), result.InputTokens, result.OutputTokens)
		ans.APIKeys[name] = key
		ans.TestModels[name] = testModel
		if !hasPreset {
			ans.ProviderBaseURLs[name] = baseURL
		}

		if !p.AskYesNo("  Add another provider?", false) {
			return
		}
	}
}

// confirmAndReview prints the [5/5] review block and asks for final approval.
func confirmAndReview(p *setupprompt.Prompt, ans *collectedAnswers) bool {
	fmt.Println("\n[5/5] Review")
	fmt.Printf("  Bind:         %s:%s\n", ans.Host, ans.Port)
	switch ans.TLSMode {
	case "autocert":
		fmt.Printf("  TLS:          Let's Encrypt (%s, redirect=%v)\n", ans.Domain, ans.Redirect)
	case "manual":
		fmt.Printf("  TLS:          Manual (%s + %s, redirect=%v)\n", ans.CertFile, ans.KeyFile, ans.Redirect)
	default:
		fmt.Println("  TLS:          Plain HTTP (NOT recommended)")
	}
	if note := autocertPort80InfoNote(ans); note != "" {
		fmt.Printf("  %s\n", note)
	}
	scopeName := "system-level (User=ccrouter)"
	if ans.Scope == svcinstall.ScopeUser {
		scopeName = "user-level (current user)"
	}
	fmt.Printf("  Service:      %s\n", scopeName)
	if ans.Scope == svcinstall.ScopeUser && os.Geteuid() == 0 && os.Getenv("SUDO_USER") != "" {
		fmt.Printf("  Note:         running under sudo — systemctl will drop to user %q via runuser\n", os.Getenv("SUDO_USER"))
	}
	names := make([]string, 0, len(ans.APIKeys))
	for n := range ans.APIKeys {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("  Providers:    %s\n", strings.Join(names, ", "))
	fmt.Println("  Auto-restart: idle enabled, backoff max 5m")

	return p.AskYesNo("\n  Write config and install service?", true)
}

// applyAnswers performs the writes + service install. Side-effecting.
func applyAnswers(ans *collectedAnswers, dryRun bool, configPathOverride string) error {
	configPath := configPathOverride
	if configPath == "" {
		configPath = config.GlobalConfigPath()
	}
	configPath, _ = filepath.Abs(configPath)

	homeDir, err := configwizard.EffectiveHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	logging.Debugf("[SETUP] env: SUDO_USER=%q SUDO_UID=%q HOME=%q EffectiveHomeDir=%q",
		os.Getenv("SUDO_USER"), os.Getenv("SUDO_UID"), os.Getenv("HOME"), homeDir)
	dataDir := filepath.Join(homeDir, ".cc-modelrouter")
	shellEnvPath := filepath.Join(dataDir, "shell_env.sh")
	// systemd's EnvironmentFile= parser does not accept the `export FOO="bar"`
	// format that shell_env.sh uses — it only accepts raw KEY=value. Write a
	// parallel file in the correct format so the unit gets real API key
	// values without breaking shell_env.sh's sourceable form (existing
	// .bashrc/.zshrc integrations depend on it).
	serviceEnvPath := filepath.Join(dataDir, "service.env")
	if ans.Scope == svcinstall.ScopeSystem {
		serviceEnvPath = "/etc/cc-modelrouter/service.env"
	}

	if !strings.HasPrefix(configPath, homeDir) {
		logging.Warnf("[SETUP] path inconsistency: configPath=%s EffectiveHomeDir=%s shellEnvPath=%s",
			configPath, homeDir, shellEnvPath)
	}

	// Load the existing config so we merge onto it rather than clobbering
	// hand-edited provider baseURLs/transformers/routes. A missing file is
	// the first-install case; any other read error is logged but tolerated
	// (we fall back to defaults and let Save write a fresh file).
	var existing *config.Config
	if cfgLoaded, err := config.Load(configPath); err == nil {
		existing = cfgLoaded
	} else if !os.IsNotExist(err) {
		logging.Warnf("[SETUP] could not load existing config at %s: %v — starting fresh", configPath, err)
	}
	cfg := buildConfig(ans, existing)

	// Resolve binary path up front so dry-run can show it.
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	binPath, _ = filepath.EvalSymlinks(binPath)

	envFileForUnit := serviceEnvPath

	installer := svcinstall.SystemdInstaller{}

	// Dry-run: print config + unit, write nothing.
	if dryRun {
		fmt.Println("\n--- config.json ---")
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))

		unitBody, rerr := installer.RenderUnit(svcinstall.InstallOptions{
			Scope:      ans.Scope,
			BinaryPath: binPath,
			ConfigPath: configPath,
			EnvFile:    envFileForUnit,
			HomeDir:    dataDir,
			DataDir:    dataDir,
		})
		if rerr != nil {
			return fmt.Errorf("render unit for dry-run: %w", rerr)
		}
		fmt.Println("\n--- ccrouter.service ---")
		fmt.Println(unitBody)
		fmt.Println("\n(dry-run: no files written, no service installed)")
		return nil
	}

	// Existing config: merge-by-default (we preserve provider baseURLs and
	// routes), but still back up before writing so the operator can roll
	// back. The backup is the safety net — the merge is supposed to be
	// non-destructive, but a .bak costs nothing and makes recovery trivial.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
		p := setupprompt.New()
		if !p.AskYesNo("  Merge setup answers into existing config?", true) {
			return fmt.Errorf("aborted: existing config not modified")
		}
		bak := configPath + ".bak." + time.Now().Format("20060102-150405")
		if err := os.Rename(configPath, bak); err != nil {
			return fmt.Errorf("back up existing config: %w", err)
		}
		fmt.Printf("  Backed up to %s\n", bak)
	}

	// Write config.json (mode 0600 via config.Save).
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("✓ Config written:    %s\n", configPath)

	// Write shell_env.sh via the shared configwizard helper so the file
	// format stays consistent with `ccrouter config`.
	shellCfg, err := configwizard.GetShellConfig()
	if err != nil {
		return fmt.Errorf("resolve shell config: %w", err)
	}
	// Guard: refuse to overwrite a populated shell_env.sh with zero keys.
	// This catches the case where the user skipped [4/5] *and* declined
	// (or never saw) the reuse prompt — e.g. shell_env.sh appeared between
	// collectAnswers and applyAnswers, or a future code path forgets to
	// populate ans.APIKeys.
	if len(ans.APIKeys) == 0 {
		if existing, err := shellCfg.LoadEnvFile(); err == nil && len(existing) > 0 {
			return fmt.Errorf("refusing to overwrite %s (%d keys) with empty provider set — re-run and accept the existing providers, or remove the file first",
				shellEnvPath, len(existing))
		}
	}
	if _, err := shellCfg.WriteEnvFile(ans.APIKeys); err != nil {
		return fmt.Errorf("write shell_env.sh: %w", err)
	}
	// configwizard.WriteEnvFile writes mode 0644 inside the 0700 home dir;
	// tighten to 0600 so only the owner can read API keys.
	if err := os.Chmod(shellEnvPath, 0600); err != nil {
		logging.Warnf("[SETUP] could not chmod %s: %v", shellEnvPath, err)
	}
	fmt.Printf("✓ API keys written:  %s (mode 0600)\n", shellEnvPath)

	if !installer.Available() {
		fmt.Println()
		fmt.Println("Note: systemd not detected on this host (or this is a dev machine).")
		fmt.Println("      Config + shell_env.sh have been written; skipping service install.")
		fmt.Println("      Re-run on the target Linux server to install the service.")
		return nil
	}

	// Write the systemd-format EnvironmentFile for both scopes. For system
	// scope this is a root-owned file the ccrouter service user reads; for
	// user scope it lives in the invoking user's home alongside shell_env.sh.
	// Without this, the unit's EnvironmentFile= directive would point at
	// shell_env.sh, whose `export FOO="bar"` format systemd silently drops —
	// leaving API key placeholders unexpanded at start.go:313-318 → exit 1.
	if err := writeEnvFileForSystemd(serviceEnvPath, ans.APIKeys, ans.Scope); err != nil {
		return fmt.Errorf("write service.env: %w", err)
	}
	switch ans.Scope {
	case svcinstall.ScopeSystem:
		fmt.Printf("✓ Service env file:  %s (root:ccrouter 0640)\n", serviceEnvPath)
	case svcinstall.ScopeUser:
		fmt.Printf("✓ Service env file:  %s (mode 0600)\n", serviceEnvPath)
	}

	if warnAboutBinaryLocation(binPath) {
		p := setupprompt.New()
		if p.AskYesNo(fmt.Sprintf("  Binary at %s looks home-built; install a copy to /usr/local/bin/ccrouter before continuing?", binPath), true) {
			newPath := "/usr/local/bin/ccrouter"
			if err := copyFileWithSudo(binPath, newPath); err != nil {
				return fmt.Errorf("copy binary: %w", err)
			}
			binPath = newPath
			fmt.Printf("✓ Binary installed:  %s\n", binPath)
		}
	}

	installOpts := svcinstall.InstallOptions{
		Scope:      ans.Scope,
		BinaryPath: binPath,
		ConfigPath: configPath,
		EnvFile:    envFileForUnit,
		HomeDir:    dataDir,
		DataDir:    dataDir,
	}
	res, err := installer.Install(installOpts)
	if err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	fmt.Printf("✓ Systemd unit:      %s\n", res.UnitPath)

	// System scope: chown config/data files so the ccrouter service user
	// can read them. Must run after Install because Install calls
	// ensureSystemUser, which creates the ccrouter group that the chown
	// depends on. Best-effort: failures are logged but non-fatal.
	if ans.Scope == svcinstall.ScopeSystem {
		fixSystemScopeOwnership(configPath, dataDir, homeDir, shellEnvPath)
	}

	// System scope: re-chown service.env now that the ccrouter group exists.
	if ans.Scope == svcinstall.ScopeSystem {
		if out, cherr := exec.Command("sudo", "chown", "root:"+svcinstall.DefaultServiceUser, serviceEnvPath).CombinedOutput(); cherr != nil {
			logging.Warnf("[SETUP] final chown failed: %s", string(out))
		}
	}

	if err := installer.Enable(installOpts, res.UnitPath); err != nil {
		fmt.Printf("⚠ Enable failed: %v\n", err)
		scopeFlagManual := ""
		if ans.Scope == svcinstall.ScopeUser {
			scopeFlagManual = "--user "
		}
		fmt.Printf("  The unit file is written. Run `systemctl %sdaemon-reload && systemctl %senable --now ccrouter` manually.\n",
			scopeFlagManual, scopeFlagManual)
	} else {
		fmt.Println("✓ Service enabled + started.")

		// Verify the service actually reached "active". Enable returns
		// nil the moment systemd accepts the start transaction; a unit
		// whose ExecStart exits 1 will restart-loop forever while Enable
		// has already declared success. This poll through one RestartSec
		// cycle surfaces the real failure (config perms, :80 bind, etc.)
		// and returns non-zero so silent success is impossible.
		fmt.Println("Verifying service is active...")
		if verr := installer.VerifyActive(installOpts, 20*time.Second); verr != nil {
			fmt.Fprintf(os.Stderr, "\n⚠ Service did not reach active state:\n")
			fmt.Fprintf(os.Stderr, "%v\n\n", verr)
			fmt.Fprintf(os.Stderr, "Common causes:\n")
			fmt.Fprintf(os.Stderr, "  • Config file unreadable by ccrouter user — check: sudo -u ccrouter cat %s\n", configPath)
			fmt.Fprintf(os.Stderr, "  • Port :80 bind denied (autocert) — check cloud security group allows inbound TCP :80\n")
			fmt.Fprintf(os.Stderr, "  • Autocert cache unwritable — check: ls -la %s/letsencrypt\n", dataDir)
			scopeFlag := ""
			if ans.Scope == svcinstall.ScopeUser {
				scopeFlag = "--user "
			}
			fmt.Fprintf(os.Stderr, "\nLive logs: journalctl %s-u ccrouter -f\n", scopeFlag)
			return fmt.Errorf("service failed to activate: %w", verr)
		}
		fmt.Println("✓ Service is active.")
	}

	fmt.Println()
	fmt.Println("To apply API keys in your current shell:")
	fmt.Printf("  source %s\n", shellEnvPath)
	fmt.Println()
	scopeFlag := ""
	if ans.Scope == svcinstall.ScopeUser {
		scopeFlag = "--user "
	}
	fmt.Printf("Service status: systemctl %sstatus ccrouter\n", scopeFlag)
	fmt.Printf("Service logs:   journalctl %s-u ccrouter -f\n", scopeFlag)
	return nil
}

// buildConfig assembles a *config.Config from collected answers. API keys
// are stored as ${CCROUTER_<NAME>_API_KEY} placeholders; the real values
// live in shell_env.sh (and service.env for system scope).
//
// If `existing` is non-nil, its provider/router/auto-restart fields are
// preserved — `ans` only refreshes the APIKey placeholder and fills in
// fields that are empty. Without this merge, re-running `setup server`
// overwrites a config.json previously built by `ccrouter config` and
// drops baseURL/transformer for any custom-named providers, causing
// `ccrouter start` to fail with "baseURL is required".
func buildConfig(ans *collectedAnswers, existing *config.Config) *config.Config {
	var cfg *config.Config
	if existing != nil {
		cfg = existing
	} else {
		cfg = config.Defaults()
	}
	cfg.Server.Host = ans.Host
	var port int
	fmt.Sscanf(ans.Port, "%d", &port)
	cfg.Server.Port = port

	switch ans.TLSMode {
	case "autocert":
		cfg.Server.TLS = &config.TLSConfig{
			Domain:   ans.Domain,
			Redirect: ans.Redirect,
		}
	case "manual":
		cfg.Server.TLS = &config.TLSConfig{
			CertFile: ans.CertFile,
			KeyFile:  ans.KeyFile,
			Redirect: ans.Redirect,
		}
	}

	// Auto-restart defaults — only applied when the existing config hasn't
	// already set them, so a user-tuned restart window survives re-install.
	if cfg.Server.AutoRestartIdle == "" {
		cfg.Server.AutoRestartIdle = "1h"
	}
	if cfg.Server.AutoRestartBackoffMax == "" {
		cfg.Server.AutoRestartBackoffMax = "5m"
	}

	// Providers: merge onto existing. Preserved entries keep their
	// baseURL/transformer/models; we only refresh the APIKey placeholder
	// (the env var name is deterministic from the provider name).
	//
	// Matching is case-insensitive because the shell_env.sh → parseEnvExports
	// round-trip historically lost case (and legacy files without `# provider:`
	// comments still do). When a setup answer key matches an existing entry
	// case-insensitively, we merge into the EXISTING (original-case) key and
	// delete any lowercased phantom that may have slipped in from a prior
	// buggy run. This prevents the v0.2.12 regression where `bigmodelKNDY`
	// (existing, with baseURL) was duplicated by `bigmodelkndy` (lowercased,
	// no baseURL) and startup failed with "baseURL is required".
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.ProviderConfig{}
	}
	existingByLower := map[string]string{}
	for existingName := range cfg.Providers {
		existingByLower[strings.ToLower(existingName)] = existingName
	}
	for name := range ans.APIKeys {
		envVar := configwizard.GenerateEnvVarName(name)
		preset, hasPreset := configwizard.ProviderPresets[strings.ToLower(name)]
		targetName, hadExisting := existingByLower[strings.ToLower(name)]
		if !hadExisting {
			// New entry — use the operator's typed case verbatim so the
			// stored key reflects what the user entered.
			targetName = name
		}
		pc := cfg.Providers[targetName]
		pc.APIKey = "${" + envVar + "}"
		if hasPreset {
			// Fill in any missing fields from the preset — preserves
			// user overrides when present, fills defaults otherwise.
			if pc.BaseURL == "" {
				pc.BaseURL = preset.BaseURL
			}
			if pc.Transformer == "" {
				pc.Transformer = preset.Transformer
			}
			if len(pc.Models) == 0 {
				pc.Models = preset.Models
			}
		} else if !hadExisting {
			// Fresh custom provider with no preset and no existing entry.
			// Use the test model so the entry isn't empty; apply the
			// operator-entered baseURL; default transformer to "anthropic"
			// (the common case for Anthropic-compatible gateways).
			if m := ans.TestModels[name]; m != "" {
				pc.Models = []string{m}
			}
			if url := ans.ProviderBaseURLs[name]; url != "" {
				pc.BaseURL = url
			}
			if pc.Transformer == "" {
				pc.Transformer = "anthropic"
			}
		}
		cfg.Providers[targetName] = pc
		// If a lowercased phantom entry exists alongside the original
		// (e.g. left behind by a buggy prior run), remove it so the
		// config has exactly one canonical entry per provider.
		if hadExisting && targetName != name {
			delete(cfg.Providers, name)
		}
	}

	// Default route: only synthesize when the existing config has no
	// "default" route — preserves hand-edited chains from `ccrouter config`.
	if cfg.Router.Routes == nil {
		cfg.Router.Routes = map[string]string{}
	}
	if _, hasDefault := cfg.Router.Routes["default"]; !hasDefault && len(cfg.Providers) > 0 {
		var chain []string
		for _, name := range sortedProviderNames(cfg.Providers) {
			pc := cfg.Providers[name]
			if len(pc.Models) == 0 {
				continue
			}
			chain = append(chain, name+":"+pc.Models[0])
		}
		if len(chain) > 0 {
			cfg.Router.Routes["default"] = strings.Join(chain, ";")
		}
	}

	return cfg
}

func sortedProviderNames(m map[string]config.ProviderConfig) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// validateAutocertDomain returns "" if the input is acceptable as a Let's
// Encrypt autocert FQDN, or a human-readable reason otherwise. Used by the
// [2/5] TLS prompt loop so the user re-enters instead of crashing on start.
//
// Rules:
//   - Empty string is rejected (the autocert Manager has no domain to mint).
//   - IP literals (v4 or v6) are rejected — public ACME CAs do not issue
//     certificates for IP identifiers under the http-01 flow autocert uses.
//
// Everything else is accepted at prompt time; DNS resolution and actual
// issuance happen later when the server starts.
func validateAutocertDomain(domain string) string {
	if domain == "" {
		return "Domain is required for Let's Encrypt mode."
	}
	if ip := net.ParseIP(domain); ip != nil {
		return fmt.Sprintf("%q is an IP address — Let's Encrypt won't issue a cert for an IP via autocert. Use a DNS name, or switch to manual TLS mode.", domain)
	}
	return ""
}

// validateScopeTLSCombo rejects combinations of scope + TLS mode that will
// crash at start time. Currently this is user-scope + autocert: autocert
// needs to bind :80 for the ACME http-01 challenge, and user-scope systemd
// units run without CAP_NET_BIND_SERVICE, so the listener fails to bind
// and the service restart-loops.
//
// Returns "" if the combo is OK, else a human-readable reason that includes
// the remediation (switch to system-scope, or pick manual TLS).
func validateScopeTLSCombo(ans *collectedAnswers) string {
	if ans.Scope == svcinstall.ScopeUser && ans.TLSMode == "autocert" {
		return "user-scope systemd can't bind :80 (no CAP_NET_BIND_SERVICE) — " +
			"autocert requires it for the ACME http-01 challenge. " +
			"Pick system-scope, or switch TLS mode to Manual."
	}
	return ""
}

// autocertPort80InfoNote returns a non-blocking reminder for system-scope
// + autocert installs. System-scope is now technically OK (the unit
// template carries AmbientCapabilities=CAP_NET_BIND_SERVICE), but autocert
// still requires *inbound* TCP :80 reachability for the ACME http-01
// challenge — cloud security groups and host firewalls commonly block it
// by default. Returns "" when not applicable. The caller prints the note
// as a hint, not an error.
func autocertPort80InfoNote(ans *collectedAnswers) string {
	if ans.Scope == svcinstall.ScopeSystem && ans.TLSMode == "autocert" {
		return "Note: autocert binds :80 for the ACME http-01 challenge — " +
			"confirm inbound TCP :80 is allowed in your cloud security group " +
			"and host firewall before starting the service."
	}
	return ""
}

// fixSystemScopeOwnership chowns the config/data files so the ccrouter
// service user can read them. config.Save writes mode 0600 owned by the
// invoking user (typically `admin`); the system-scope unit runs as
// `User=ccrouter` and so cannot read /home/admin/.cc-modelrouter/config.json
// — config.Load fails with permission denied and the service restart-loops
// with exit 1. This step must run AFTER installer.Install, which calls
// ensureSystemUser to create the ccrouter group — chown admin:ccrouter
// fails before that group exists.
//
// All operations are sudo-wrapped and best-effort: failures are logged as
// warnings but do not abort the install. The operator workflow is
// preserved — admin remains the owner of every file, so
// `vim ~/.cc-modelrouter/config.json` still works. ccrouter is added as
// a group member gaining read+write access; the only group members are admin
// (already the owner) and the ccrouter service account.
//
// Data-dir mode is 0770 (not 0750) because the ccrouter service user,
// landed in group `ccrouter`, needs WRITE access to usage.db, logs/,
// instances/, autocert cache, and master.key — all of which the service
// creates or updates at runtime. A 0750 would grant only r-x to the
// group and every write would fail with EACCES, restart-looping the
// service. The two dedicated chmod steps for config.json (0640) and
// shell_env.sh (0600) below override the recursive 0660 for those
// specific files.
//
// /home/admin is chmod 0711 (not 0750) so the ccrouter user can traverse
// into it without being able to list admin's other files.
func fixSystemScopeOwnership(configPath, dataDir, homeDir, shellEnvPath string) {
	invokingUser := os.Getenv("SUDO_USER")
	if invokingUser == "" {
		invokingUser = os.Getenv("USER")
	}
	if invokingUser == "" {
		logging.Warnf("[SETUP] fixSystemScopeOwnership: could not resolve invoking user; skipping ownership fix")
		return
	}
	group := svcinstall.DefaultServiceUser

	type step struct {
		desc string
		args []string
	}
	steps := []step{
		{"chown data dir", []string{"chown", "-R", invokingUser + ":" + group, dataDir}},
		{"chmod data dir", []string{"chmod", "0770", dataDir}},
		{"chmod data subdirs", []string{"find", dataDir, "-mindepth", "1", "-type", "d", "-exec", "chmod", "0770", "{}", "+"}},
		{"chmod data files", []string{"find", dataDir, "-mindepth", "1", "-type", "f", "-exec", "chmod", "0660", "{}", "+"}},
		{"chown config", []string{"chown", invokingUser + ":" + group, configPath}},
		{"chmod config", []string{"chmod", "0640", configPath}},
		{"chown shell_env.sh", []string{"chown", invokingUser + ":" + group, shellEnvPath}},
		{"chmod shell_env.sh", []string{"chmod", "0600", shellEnvPath}},
		{"chmod home traversal", []string{"chmod", "0711", homeDir}},
	}
	// shell_env.sh may not exist yet on a fresh install where the user
	// declined all providers; tolerate its absence.
	for _, s := range steps {
		if s.desc == "chown shell_env.sh" || s.desc == "chmod shell_env.sh" {
			if _, err := os.Stat(shellEnvPath); err != nil {
				continue
			}
		}
		out, err := exec.Command("sudo", s.args...).CombinedOutput()
		if err != nil {
			logging.Warnf("[SETUP] %s failed: %s (%s)", s.desc, strings.TrimSpace(string(out)), err)
		}
	}
	logging.Infof("[SETUP] ownership fixed: %s owned by %s:%s, %s chmod 0711", dataDir, invokingUser, group, homeDir)
}

// writeEnvFileForSystemd writes an env file in systemd's EnvironmentFile=
// format (raw KEY=value, no export prefix, no shell quotes). Used for both
// system scope (/etc/cc-modelrouter/service.env, root:ccrouter 0640 via sudo)
// and user scope (~/.cc-modelrouter/service.env, owned by the invoking user,
// 0600). shell_env.sh stays shell-sourceable for interactive shells; this
// file is what the unit's EnvironmentFile= directive reads because systemd
// does not run a shell and silently drops lines that don't match KEY=value.
func writeEnvFileForSystemd(path string, apiKeys map[string]string, scope svcinstall.Scope) error {
	var lines []string
	for name, key := range apiKeys {
		if key == "" {
			continue
		}
		envVar := configwizard.GenerateEnvVarName(name)
		lines = append(lines, fmt.Sprintf("%s=%s", envVar, key))
	}
	content := strings.Join(lines, "\n") + "\n"

	if scope == svcinstall.ScopeUser {
		// Inside the invoking user's home — write directly. Mode 0600: the
		// file contains plaintext API keys and lives in a directory that
		// may be group-readable depending on distro defaults.
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fmt.Errorf("mkdir for %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	}

	// System scope: write via sudo tee (file is root-owned, group ccrouter).
	// Content is piped through stdin so the key never appears in process
	// argv / shell history.
	if out, err := exec.Command("sudo", "mkdir", "-p", filepath.Dir(path)).CombinedOutput(); err != nil {
		return fmt.Errorf("sudo mkdir: %w (%s)", err, string(out))
	}
	tee := exec.Command("sudo", "tee", path)
	tee.Stdin = strings.NewReader(content)
	if out, err := tee.CombinedOutput(); err != nil {
		return fmt.Errorf("sudo tee: %w (%s)", err, string(out))
	}
	if out, err := exec.Command("sudo", "chmod", "0640", path).CombinedOutput(); err != nil {
		return fmt.Errorf("sudo chmod: %w (%s)", err, string(out))
	}
	// chown to root:ccrouter may fail before ensureSystemUser has run;
	// apply again after Install. Best-effort here.
	_, _ = exec.Command("sudo", "chown", "root:"+svcinstall.DefaultServiceUser, path).CombinedOutput()
	return nil
}

// warnAboutBinaryLocation returns true if binPath is in $HOME or /tmp —
// locations that might not survive a homedir wipe or be readable by the
// service user.
func warnAboutBinaryLocation(binPath string) bool {
	if strings.HasPrefix(binPath, "/tmp/") {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		if strings.HasPrefix(binPath, home+"/") {
			return true
		}
	}
	return false
}

// copyFileWithSudo copies src to dst via sudo + tee, then sets mode 0755
// and root ownership.
func copyFileWithSudo(src, dst string) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source binary: %w", err)
	}
	tee := exec.Command("sudo", "tee", dst)
	tee.Stdin = strings.NewReader(string(srcData))
	if out, err := tee.CombinedOutput(); err != nil {
		return fmt.Errorf("sudo tee %s: %w (%s)", dst, err, string(out))
	}
	for _, args := range [][]string{
		{"sudo", "chmod", "0755", dst},
		{"sudo", "chown", "root:root", dst},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", strings.Join(args, " "), err, string(out))
		}
	}
	return nil
}
