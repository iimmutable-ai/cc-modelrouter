package svcinstall

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// DefaultServiceUser is the unprivileged account provisioned for the
// system-scope service when the caller doesn't supply one.
const DefaultServiceUser = "ccrouter"

// SystemdInstaller writes a systemd unit and drives systemctl.
//
// The struct has no fields because all configuration comes in via
// InstallOptions on each method call. A zero value is ready to use.
type SystemdInstaller struct {
	// run is the command runner. It's a function field so tests can
	// substitute a fake instead of touching the real systemctl binary.
	// Nil means use exec.Command.
	run func(name string, args ...string) ([]byte, error)
}

// commandRunner is the package-default runner used when SystemdInstaller.run
// is nil. It's a thin wrapper around exec.Command that captures stderr.
func commandRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (s SystemdInstaller) runner() func(string, ...string) ([]byte, error) {
	if s.run != nil {
		return s.run
	}
	return commandRunner
}

// Available reports whether systemd is present. We look for systemctl
// in PATH and require it to be on Linux — systemd-on-macOS is not a
// real thing even if a binary named systemctl happens to exist.
func (s SystemdInstaller) Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	out, err := s.runner()("systemctl", "--version")
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(out), "systemd ")
}

// renderUnit produces the unit file body. Pure function — callers can
// use this directly in tests without touching the filesystem.
func renderUnit(opts InstallOptions) (string, error) {
	tf := templateFields{
		BinaryPath: opts.BinaryPath,
		ConfigPath: opts.ConfigPath,
		EnvFile:    opts.EnvFile,
		HomeDir:    opts.HomeDir,
		DataDir:    opts.DataDir,
		Target:     "multi-user.target",
	}
	if opts.Scope == ScopeUser {
		tf.Target = "default.target"
		// User-scope units run as the invoking user; User=/Group= are
		// invalid in this context.
	} else {
		tf.User = opts.User
		tf.Group = opts.Group
		if tf.User == "" {
			tf.User = DefaultServiceUser
		}
		if tf.Group == "" {
			tf.Group = tf.User
		}
	}
	var buf bytes.Buffer
	if err := unitTemplate.Execute(&buf, tf); err != nil {
		return "", fmt.Errorf("render unit template: %w", err)
	}
	return buf.String(), nil
}

// unitPathFor returns the canonical systemd unit path for a scope.
// System: /etc/systemd/system/ccrouter.service
// User:   ~/.config/systemd/user/ccrouter.service (resolved via
//         config.EffectiveHomeDir so bare `sudo` writes to the invoking
//         user's home, not /root).
func unitPathFor(scope Scope) (string, error) {
	const fname = "ccrouter.service"
	if scope == ScopeSystem {
		return filepath.Join("/etc/systemd/system", fname), nil
	}
	home, err := config.EffectiveHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for user-scope unit: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", fname), nil
}

// writeFileWithMode writes content to path with the given mode, creating
// parent directories as needed.
func writeFileWithMode(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	return os.WriteFile(path, []byte(content), mode)
}

// ensureSystemUser provisions the runtime user for ScopeSystem. If the
// user already exists (id -u succeeds), this is a no-op. If not, we run
// useradd under sudo — sudo will prompt for a password if the invoking
// user isn't passwordless-sudo.
func (s SystemdInstaller) ensureSystemUser(user string) error {
	if user == "" {
		user = DefaultServiceUser
	}
	// id -u is a cheap, dependency-free check. Exit 0 means the user
	// already exists; non-zero means we need to create it.
	if _, err := s.runner()("id", "-u", user); err == nil {
		return nil
	}
	homeDir := "/var/lib/" + user
	args := []string{
		"--system",
		"--shell", "/usr/sbin/nologin",
		"--home-dir", homeDir,
		"--create-home",
		user,
	}
	// Try non-interactive sudo first; if that fails (password needed),
	// retry without -n so sudo can actually prompt the user.
	if _, err := s.runner()("sudo", append([]string{"-n", "useradd"}, args...)...); err != nil {
		if out, err2 := s.runner()("sudo", append([]string{"useradd"}, args...)...); err2 != nil {
			return fmt.Errorf("create system user %q: %w (output: %s)", user, err2, string(out))
		}
	}
	return nil
}

// RenderUnit returns the unit file body without writing anything. Useful
// for dry-run display and for tests that need to inspect the rendered
// body without touching the filesystem.
func (s SystemdInstaller) RenderUnit(opts InstallOptions) (string, error) {
	return renderUnit(opts)
}

// Install writes the unit file (and provisions the service user for
// ScopeSystem). It does not enable or start the service.
func (s SystemdInstaller) Install(opts InstallOptions) (InstallResult, error) {
	if !s.Available() {
		return InstallResult{}, fmt.Errorf("systemd not available on this host")
	}
	if opts.Scope == ScopeSystem {
		if err := s.ensureSystemUser(opts.User); err != nil {
			return InstallResult{}, err
		}
	}
	body, err := renderUnit(opts)
	if err != nil {
		return InstallResult{}, err
	}
	unitPath, err := unitPathFor(opts.Scope)
	if err != nil {
		return InstallResult{}, err
	}
	if err := writeFileWithMode(unitPath, body, 0644); err != nil {
		return InstallResult{}, err
	}
	res := InstallResult{UnitPath: unitPath}
	if opts.Scope == ScopeSystem {
		res.User = opts.User
		if res.User == "" {
			res.User = DefaultServiceUser
		}
		res.Group = res.User
	}
	return res, nil
}

// systemctlArgs prefixes --user for ScopeUser.
func systemctlArgs(scope Scope, args ...string) []string {
	if scope == ScopeUser {
		return append([]string{"--user"}, args...)
	}
	return args
}

// Enable performs daemon-reload, enable, and start. Each is a separate
// call so partial failures produce an error message that identifies
// which step failed.
func (s SystemdInstaller) Enable(opts InstallOptions, unitPath string) error {
	const serviceName = "ccrouter"
	if _, err := s.runner()("systemctl", systemctlArgs(opts.Scope, "daemon-reload")...); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if _, err := s.runner()("systemctl", systemctlArgs(opts.Scope, "enable", "--now", serviceName)...); err != nil {
		return fmt.Errorf("systemctl enable --now %s: %w", serviceName, err)
	}
	return nil
}

// Uninstall stops, disables, and removes the unit file. It leaves the
// service user and data directories intact — removing users is a
// privileged decision that belongs to the operator.
func (s SystemdInstaller) Uninstall(scope Scope) error {
	const serviceName = "ccrouter"
	// stop+disable are best-effort: a not-yet-installed service shouldn't
	// fail uninstall. We only error on the actual file removal.
	_, _ = s.runner()("systemctl", systemctlArgs(scope, "stop", serviceName)...)
	_, _ = s.runner()("systemctl", systemctlArgs(scope, "disable", serviceName)...)
	unitPath, err := unitPathFor(scope)
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	_, _ = s.runner()("systemctl", systemctlArgs(scope, "daemon-reload")...)
	return nil
}
