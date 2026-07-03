package svcinstall

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUnit_SystemScope_HasHardening(t *testing.T) {
	opts := InstallOptions{
		Scope:      ScopeSystem,
		BinaryPath: "/usr/local/bin/ccrouter",
		ConfigPath: "/home/admin/.cc-modelrouter/config.json",
		EnvFile:    "/etc/cc-modelrouter/service.env",
		HomeDir:    "/var/lib/ccrouter",
		DataDir:    "/var/lib/ccrouter",
	}
	body, err := renderUnit(opts)
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}

	checks := []string{
		"User=ccrouter",
		"Group=ccrouter",
		"ExecStart=/usr/local/bin/ccrouter start --config /home/admin/.cc-modelrouter/config.json",
		"WorkingDirectory=/var/lib/ccrouter",
		"EnvironmentFile=/etc/cc-modelrouter/service.env",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/ccrouter",
		"WantedBy=multi-user.target",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("unit body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestRenderUnit_UserScope_OmitsHardening(t *testing.T) {
	opts := InstallOptions{
		Scope:      ScopeUser,
		BinaryPath: "/usr/local/bin/ccrouter",
		ConfigPath: "/home/admin/.cc-modelrouter/config.json",
		EnvFile:    "/home/admin/.cc-modelrouter/shell_env.sh",
		HomeDir:    "/home/admin",
		DataDir:    "/home/admin/.cc-modelrouter",
	}
	body, err := renderUnit(opts)
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}

	if strings.Contains(body, "User=") {
		t.Errorf("user-scope unit must not set User=, but does:\n%s", body)
	}
	if strings.Contains(body, "NoNewPrivileges=") {
		t.Errorf("user-scope unit must not set NoNewPrivileges=, but does:\n%s", body)
	}
	if !strings.Contains(body, "WantedBy=default.target") {
		t.Errorf("user-scope unit must target default.target; got:\n%s", body)
	}
}

func TestRenderUnit_CustomUser(t *testing.T) {
	opts := InstallOptions{
		Scope:      ScopeSystem,
		BinaryPath: "/usr/local/bin/ccrouter",
		ConfigPath: "/etc/cc-modelrouter/config.json",
		EnvFile:    "/etc/cc-modelrouter/service.env",
		HomeDir:    "/var/lib/ccrouter",
		DataDir:    "/var/lib/ccrouter",
		User:       "cliobot",
	}
	body, err := renderUnit(opts)
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}
	if !strings.Contains(body, "User=cliobot") || !strings.Contains(body, "Group=cliobot") {
		t.Errorf("custom user not honored; got:\n%s", body)
	}
}

func TestUnitPathFor(t *testing.T) {
	got, err := unitPathFor(ScopeSystem)
	if err != nil {
		t.Fatalf("unitPathFor(system): %v", err)
	}
	if got != "/etc/systemd/system/ccrouter.service" {
		t.Errorf("system unit path = %s; want /etc/systemd/system/ccrouter.service", got)
	}

	home, _ := os.UserHomeDir()
	got, err = unitPathFor(ScopeUser)
	if err != nil {
		t.Fatalf("unitPathFor(user): %v", err)
	}
	want := filepath.Join(home, ".config", "systemd", "user", "ccrouter.service")
	if got != want {
		t.Errorf("user unit path = %s; want %s", got, want)
	}
}

// TestUnitPathFor_SudoUser verifies that when SUDO_USER is set (bare sudo,
// no -E), unitPathFor(ScopeUser) resolves the unit under the invoking
// user's home rather than /root. System scope must be unaffected.
func TestUnitPathFor_SudoUser(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}

	// User scope under SUDO_USER=current user → /home/<user>/.config/...
	t.Setenv("SUDO_USER", current.Username)
	got, err := unitPathFor(ScopeUser)
	if err != nil {
		t.Fatalf("unitPathFor(user, sudo): %v", err)
	}
	want := filepath.Join(current.HomeDir, ".config", "systemd", "user", "ccrouter.service")
	if got != want {
		t.Errorf("user unit path under SUDO_USER=%s = %s; want %s",
			current.Username, got, want)
	}

	// System scope must stay at /etc/... regardless of SUDO_USER.
	gotSys, err := unitPathFor(ScopeSystem)
	if err != nil {
		t.Fatalf("unitPathFor(system, sudo): %v", err)
	}
	if gotSys != "/etc/systemd/system/ccrouter.service" {
		t.Errorf("system unit path under SUDO_USER = %s; want /etc/systemd/system/ccrouter.service",
			gotSys)
	}

	// User scope with nonexistent SUDO_USER falls back to os.UserHomeDir().
	t.Setenv("SUDO_USER", "nonexistentuser_ccrouter_test_zzz")
	osHome, _ := os.UserHomeDir()
	gotFallback, err := unitPathFor(ScopeUser)
	if err != nil {
		t.Fatalf("unitPathFor(user, fallback): %v", err)
	}
	wantFallback := filepath.Join(osHome, ".config", "systemd", "user", "ccrouter.service")
	if gotFallback != wantFallback {
		t.Errorf("user unit path under bogus SUDO_USER = %s; want %s (fallback)",
			gotFallback, wantFallback)
	}
}

// fakeRunner returns canned output for specific command lines, and a
// generic error for unknown ones. Lets us exercise SystemdInstaller
// without a real systemctl binary.
func fakeRunner(outputs map[string][]byte) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if out, ok := outputs[key]; ok {
			return out, nil
		}
		return nil, os.ErrInvalid
	}
}

func TestAvailable_NonLinuxDoesNotPanic(t *testing.T) {
	s := SystemdInstaller{run: fakeRunner(map[string][]byte{
		"systemctl --version": []byte("systemd 245\n"),
	})}
	_ = s.Available()
}

func TestWriteFileWithMode_CreatesDirsAndSetsPerm(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nested", "dir", "file.txt")
	if err := writeFileWithMode(path, "hello", 0644); err != nil {
		t.Fatalf("writeFileWithMode: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("perm = %v; want 0644", info.Mode().Perm())
	}
}

func TestInstall_SystemScope_RendersUnit(t *testing.T) {
	// Exercise renderUnit + writeFileWithMode end-to-end against a tmp
	// path. We can't easily redirect /etc/systemd/system from a test, so
	// this covers the same code paths Install uses minus the real
	// systemctl calls.
	body, err := renderUnit(InstallOptions{
		Scope:      ScopeSystem,
		BinaryPath: "/usr/local/bin/ccrouter",
		ConfigPath: "/tmp/config.json",
		EnvFile:    "/tmp/service.env",
		HomeDir:    "/var/lib/ccrouter",
		DataDir:    "/var/lib/ccrouter",
	})
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}
	tmp := t.TempDir()
	unitPath := filepath.Join(tmp, "ccrouter.service")
	if err := writeFileWithMode(unitPath, body, 0644); err != nil {
		t.Fatalf("writeFileWithMode: %v", err)
	}
	data, _ := os.ReadFile(unitPath)
	if !strings.Contains(string(data), "[Unit]") || !strings.Contains(string(data), "[Service]") {
		t.Errorf("unit body missing required sections; got:\n%s", string(data))
	}
}
