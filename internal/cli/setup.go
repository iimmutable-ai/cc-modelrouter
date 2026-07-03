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
		APIKeys:    map[string]string{},
		TestModels: map[string]string{},
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
		ans.Domain = p.AskString("  Domain (FQDN)", "")
		for ans.Domain == "" {
			fmt.Println("  Domain is required for Let's Encrypt mode.")
			ans.Domain = p.AskString("  Domain (FQDN)", "")
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
		} else {
			fmt.Printf("  No preset for %q — you'll need to provide baseURL + model via `ccrouter config` later.\n", name)
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
		var baseURL string
		if hasPreset {
			baseURL = preset.BaseURL
		}
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
			}
			continue
		}
		fmt.Printf("  ✓ OK (%dms, in=%d out=%d tokens)\n",
			result.Latency.Milliseconds(), result.InputTokens, result.OutputTokens)
		ans.APIKeys[name] = key
		ans.TestModels[name] = testModel

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
	scopeName := "system-level (User=ccrouter)"
	if ans.Scope == svcinstall.ScopeUser {
		scopeName = "user-level (current user)"
	}
	fmt.Printf("  Service:      %s\n", scopeName)
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	dataDir := filepath.Join(homeDir, ".cc-modelrouter")
	shellEnvPath := filepath.Join(dataDir, "shell_env.sh")
	serviceEnvPath := "/etc/cc-modelrouter/service.env"

	cfg := buildConfig(ans)

	// Resolve binary path up front so dry-run can show it.
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	binPath, _ = filepath.EvalSymlinks(binPath)

	envFileForUnit := shellEnvPath
	if ans.Scope == svcinstall.ScopeSystem {
		envFileForUnit = serviceEnvPath
	}

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

	// Existing config: prompt + backup before overwriting.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
		p := setupprompt.New()
		if !p.AskYesNo("  Overwrite?", false) {
			return fmt.Errorf("aborted: existing config not overwritten")
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

	// System-scope needs a root-owned env file the service user can read.
	if ans.Scope == svcinstall.ScopeSystem {
		if err := writeServiceEnvFile(serviceEnvPath, ans.APIKeys); err != nil {
			return fmt.Errorf("write service.env: %w", err)
		}
		fmt.Printf("✓ Service env file:  %s (root:ccrouter 0640)\n", serviceEnvPath)
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

	// System scope: re-chown service.env now that the ccrouter group exists.
	if ans.Scope == svcinstall.ScopeSystem {
		if out, cherr := exec.Command("sudo", "chown", "root:"+svcinstall.DefaultServiceUser, serviceEnvPath).CombinedOutput(); cherr != nil {
			logging.Warnf("[SETUP] final chown failed: %s", string(out))
		}
	}

	if err := installer.Enable(installOpts, res.UnitPath); err != nil {
		fmt.Printf("⚠ Enable failed: %v\n", err)
		fmt.Println("  The unit file is written. Run `systemctl daemon-reload && systemctl enable --now ccrouter` manually.")
	} else {
		fmt.Println("✓ Service enabled + started.")
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
func buildConfig(ans *collectedAnswers) *config.Config {
	cfg := config.Defaults()
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

	// Auto-restart defaults — match what ScreenServer ships.
	cfg.Server.AutoRestartIdle = "1h"
	cfg.Server.AutoRestartBackoffMax = "5m"

	// Providers
	cfg.Providers = map[string]config.ProviderConfig{}
	for name := range ans.APIKeys {
		envVar := configwizard.GenerateEnvVarName(name)
		preset, hasPreset := configwizard.ProviderPresets[name]
		pc := config.ProviderConfig{
			APIKey:  "${" + envVar + "}",
			BaseURL: preset.BaseURL,
			Models:  preset.Models,
		}
		if hasPreset {
			pc.Transformer = preset.Transformer
		} else {
			// No preset → no models list. Use the test model so the
			// entry isn't empty; the user can refine via `ccrouter config`.
			if m := ans.TestModels[name]; m != "" {
				pc.Models = []string{m}
			}
		}
		cfg.Providers[name] = pc
	}

	// Default route: chain of every configured provider's first model.
	if len(cfg.Providers) > 0 {
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

// writeServiceEnvFile writes /etc/cc-modelrouter/service.env with mode
// 0640 owned by root:ccrouter. Done via sudo tee because the cli process
// is usually not root. Content is piped through stdin so the key never
// appears in process argv / shell history.
func writeServiceEnvFile(path string, apiKeys map[string]string) error {
	var lines []string
	for name, key := range apiKeys {
		envVar := configwizard.GenerateEnvVarName(name)
		lines = append(lines, fmt.Sprintf("%s=%s", envVar, key))
	}
	content := strings.Join(lines, "\n") + "\n"

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
