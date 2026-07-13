package svcinstall

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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
// is nil. CombinedOutput captures stderr into the returned byte slice so
// callers can include it in error messages — without this, failed
// systemctl invocations surface as bare "exit status 1" with no diagnostic.
func commandRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// wrapRunnerErr formats a runner failure with the captured output trimmed
// of trailing whitespace. Used by Enable/Uninstall so callers see
// systemctl's actual complaint (e.g. "Failed to connect to bus") instead
// of a bare exit status.
//
// Three forms:
//   - LookPath failure (*exec.Error): friendly "not found in PATH" hint,
//     since the bare "exit status 1" form otherwise hides the real cause.
//   - Empty output: appends " (no output)" so the bare form is at least
//     self-describing instead of looking like an unannotated status code.
//   - Non-empty output: appends " (output: <trimmed>)".
func wrapRunnerErr(action string, out []byte, err error) error {
	trimmed := strings.TrimSpace(string(out))
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return fmt.Errorf("%s: %q not found in PATH — install util-linux (provides runuser) or re-run setup without sudo", action, execErr.Name)
	}
	if trimmed == "" {
		return fmt.Errorf("%s: %w (no output)", action, err)
	}
	return fmt.Errorf("%s: %w (output: %s)", action, err, trimmed)
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
// parent directories as needed. On any permission error — whether from
// MkdirAll (parent root-owned) or WriteFile (existing root-owned file) —
// falls back to sudo (mkdir + tee + chmod, plus best-effort chown back to
// the invoking user so we don't leave root-owned files in their home).
// May trigger an interactive sudo prompt; call only from a terminal context.
func writeFileWithMode(path, content string, mode os.FileMode) error {
	dir := filepath.Dir(path)

	// Phase 1: try direct mkdir + write. We need to distinguish "permission
	// error" (escalate) from "other error" (surface immediately) for both
	// calls, so the boolean flow control below is deliberate.
	mkdirErr := os.MkdirAll(dir, 0755)
	if mkdirErr != nil && !os.IsPermission(mkdirErr) {
		return fmt.Errorf("create dir for %s: %w", path, mkdirErr)
	}

	var writeErr error
	if mkdirErr == nil {
		writeErr = os.WriteFile(path, []byte(content), mode)
		if writeErr == nil {
			return nil
		}
		if !os.IsPermission(writeErr) {
			return fmt.Errorf("write %s: %w", path, writeErr)
		}
	}

	// Phase 2: either mkdir or write hit EACCES — escalate via sudo. Try
	// non-interactive first (fails immediately if a password is needed),
	// then retry with interactive sudo so the user can enter a password.
	if _, err := exec.LookPath("sudo"); err != nil {
		if mkdirErr != nil {
			return fmt.Errorf("create dir for %s: %w — re-run with sudo", path, mkdirErr)
		}
		return fmt.Errorf("write %s: %w — re-run with sudo", path, writeErr)
	}

	// sudoWrite runs a sudo command: try -n first (non-interactive), then
	// fall back to interactive if a password is needed.
	sudoWrite := func(args ...string) error {
		cmdArgs := append([]string{"-n"}, args...)
		if _, err := exec.Command("sudo", cmdArgs...).CombinedOutput(); err != nil {
			if _, err := exec.Command("sudo", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("sudo %s: %w", strings.Join(args, " "), err)
			}
		}
		return nil
	}

	if err := sudoWrite("mkdir", "-p", dir); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	if err := sudoWriteTee(path, content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := sudoWrite("chmod", fmt.Sprintf("%04o", mode), path); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}

	// Phase 3: best-effort ownership fix-up. When sudo created the dir/file,
	// it ended up root-owned even though it lives in the invoking user's home.
	// A prior sudo install leaving ~/.config/systemd/ root-owned is exactly
	// what blocked later non-sudo installs; restoring ownership breaks that
	// loop. chownTargetFor returns empty when the path isn't under the
	// invoking user's home (e.g. /etc/systemd/system for ScopeSystem), so
	// system-scope installs are left alone.
	if owner := chownTargetFor(dir); owner != "" {
		_ = sudoWrite("chown", "-R", owner, dir)
	}
	return nil
}

// chownTargetFor returns the "uid:gid" string to chown back to, or empty
// string if no chown is needed or safe. Called from writeFileWithMode
// Phase 3 after sudo escalation writes a file into the invoking user's
// home — we want to restore ownership so a prior root-owned state doesn't
// block future non-sudo installs (the v0.2.10 → v0.2.11 bug).
//
// Returns the chown target when:
//   - SUDO_USER is set (bare sudo wraps the whole process), OR
//   - the current OS user is non-root (covers nested-sudo: writeFileWithMode
//     escalated internally without SUDO_USER set on the parent process), AND
//   - dir is under the resolved user's home directory.
//
// The home-dir guard is load-bearing. Without it, a ScopeSystem install
// where writeFileWithMode escalates via nested sudo would chown
// /etc/systemd/system to the invoking user — a security disaster that
// would make every system unit user-writable.
func chownTargetFor(dir string) string {
	var uid, gid, home string
	if name := os.Getenv("SUDO_USER"); name != "" {
		if u, err := user.Lookup(name); err == nil {
			uid, gid, home = u.Uid, u.Gid, u.HomeDir
		}
	}
	if uid == "" {
		if u, err := user.Current(); err == nil && u.Uid != "0" {
			uid, gid, home = u.Uid, u.Gid, u.HomeDir
		}
	}
	if uid == "" || home == "" {
		return ""
	}
	// Guard: only chown if dir is under the target user's home. Rejects
	// /etc/systemd/system, /usr/local/bin, etc. filepath.Rel cleans ".."
	// and any leading slashes, so a dir outside home produces either a
	// ".."-prefixed rel path or an error.
	rel, err := filepath.Rel(home, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return uid + ":" + gid
}

// sudoWriteTee pipes content to `sudo tee <path>`, trying -n first then
// interactive. Content goes through stdin — never in process argv.
func sudoWriteTee(path, content string) error {
	// Try non-interactive first.
	tee := exec.Command("sudo", "-n", "tee", path)
	tee.Stdin = strings.NewReader(content)
	if _, err := tee.CombinedOutput(); err == nil {
		return nil
	}
	// Non-interactive failed (password needed); retry interactively.
	tee = exec.Command("sudo", "tee", path)
	tee.Stdin = strings.NewReader(content)
	_, err := tee.CombinedOutput()
	return err
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

// systemctlArgs prefixes --user for ScopeUser. Used only for the
// non-sudo path (system scope, or user scope running as the actual
// invoking user). The sudo path uses buildSystemctlCommand instead.
func systemctlArgs(scope Scope, args ...string) []string {
	if scope == ScopeUser {
		return append([]string{"--user"}, args...)
	}
	return args
}

// userUnderSudo reports whether a user-scope install is running under
// bare sudo. The check takes euid and sudoUser as parameters so the
// decision is testable without actually running the test process as
// root; production callers pass os.Geteuid() and os.Getenv("SUDO_USER").
//
// In that case user-scope systemctl must run via `runuser -u $SUDO_USER`
// so it can reach the invoking user's user-systemd instance; root cannot
// talk to another UID's /run/user/<uid>/systemd/private socket directly.
func userUnderSudo(scope Scope, euid int, sudoUser string) bool {
	return scope == ScopeUser && euid == 0 && sudoUser != ""
}

// buildSystemctlCommand returns the program name and argv (without the
// program name) for invoking systemctl in the given scope.
//
// System scope, or user scope without sudo, is a plain
// `systemctl [--user] <args>`.
//
// User scope under bare sudo becomes
// `runuser -u <sudoUser> -- env XDG_RUNTIME_DIR=/run/user/<uid>
//     DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/<uid>/bus
//     systemctl --user <args>`
// so systemctl inherits the invoking user's runtime dir AND D-Bus session
// bus address, letting it talk to their user-systemd instance. Both env
// vars are required on modern systemd — XDG_RUNTIME_DIR alone is not
// enough to locate the session bus. We use `/usr/bin/env` to set them
// only for the systemctl subprocess rather than mutating the parent Go
// process's environment.
//
// euid and sudoUser are passed in (rather than read from the OS) so the
// command shape is deterministic to test.
func buildSystemctlCommand(scope Scope, euid int, sudoUser string, userLookup func(string) (*user.User, error), args ...string) (string, []string, error) {
	if !userUnderSudo(scope, euid, sudoUser) {
		return "systemctl", systemctlArgs(scope, args...), nil
	}
	u, err := userLookup(sudoUser)
	if err != nil {
		return "", nil, fmt.Errorf("lookup SUDO_USER %q to drop privileges for user-scope systemctl: %w — re-run without sudo or pick system scope", sudoUser, err)
	}
	if _, perr := strconv.Atoi(u.Uid); perr != nil {
		return "", nil, fmt.Errorf("SUDO_USER %q has non-numeric uid %q", sudoUser, u.Uid)
	}
	xrd := "/run/user/" + u.Uid
	argv := []string{
		"-u", sudoUser, "--",
		"env",
		"XDG_RUNTIME_DIR=" + xrd,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + xrd + "/bus",
		"systemctl", "--user",
	}
	argv = append(argv, args...)
	return "runuser", argv, nil
}

// systemctlUserUnderSudo is the production entry point that reads the
// real OS state. Tests exercise buildSystemctlCommand + userUnderSudo
// directly instead.
func systemctlUserUnderSudo(scope Scope) bool {
	return userUnderSudo(scope, os.Geteuid(), os.Getenv("SUDO_USER"))
}

// buildSystemctlCommandForProd resolves the euid/SUDO_USER at call time
// and uses the real user.Lookup. This is the call site used by Enable
// and Uninstall; tests use buildSystemctlCommand directly.
func buildSystemctlCommandForProd(scope Scope, args ...string) (string, []string, error) {
	return buildSystemctlCommand(scope, os.Geteuid(), os.Getenv("SUDO_USER"), user.Lookup, args...)
}

// runSystemctlCmdEscalating runs `systemctl <args>` via buildSystemctlCommandForProd
// for scope, escalating to sudo when ScopeSystem is being driven by a
// non-root invoking user. The non-sudo path covers ScopeUser (which talks
// to the user manager directly) and actual-root invocations (which don't
// need sudo to reach the system manager).
//
// Why sudo at all: `systemctl daemon-reload` and `systemctl enable --now`
// for a system-scope unit write to /run/systemd/system and read
// /etc/systemd/system — both require root, and polkit typically refuses
// them without an interactive auth agent. When ccrouter is invoked from a
// non-root SSH session (the common case for `ccrouter setup server` on a
// fresh box), `systemctl` returns "Interactive authentication required."
// sudo is the universal escape hatch.
//
// The sudo wrapper mirrors enableLinger's pattern (lines ~602-618):
//
//  1. Try `sudo -n` first — succeeds immediately on passwordless-sudo
//     boxes, fails fast otherwise.
//  2. Fall back to interactive `sudo` so the user can actually type a
//     password.
//
// `run` is the command runner (real or fake). The action string is built
// from the un-escalated systemctl args so error messages reference the
// systemctl action, not "sudo -n systemctl ...".
func runSystemctlCmdEscalating(
	run func(name string, args ...string) ([]byte, error),
	scope Scope, args ...string,
) error {
	cmdName, cmdArgs, err := buildSystemctlCommandForProd(scope, args...)
	if err != nil {
		return err
	}
	action := fmt.Sprintf("%s %s", cmdName, strings.Join(cmdArgs, " "))

	if !needsSudo(scope) {
		out, err := run(cmdName, cmdArgs...)
		if err != nil {
			return wrapRunnerErr(action, out, err)
		}
		return nil
	}

	// ScopeSystem + non-root: try sudo -n first, then interactive sudo.
	// We don't pre-check for the binary — if sudo is missing, the first
	// exec will return an *exec.Error and wrapRunnerErr surfaces a useful
	// message ("sudo not found in PATH — re-run with sudo").
	sudoArgs := append([]string{"-n"}, append([]string{cmdName}, cmdArgs...)...)
	if _, err := run("sudo", sudoArgs...); err == nil {
		return nil
	}
	sudoArgs = append([]string{cmdName}, cmdArgs...)
	out, err := run("sudo", sudoArgs...)
	if err != nil {
		return wrapRunnerErr("sudo "+action, out, err)
	}
	return nil
}

// needsSudo reports whether a ScopeSystem install running as the current
// process requires sudo escalation to talk to systemctl. The two cases
// that DON'T need it: actual root (euid 0) and bare-sudo (euid 0 with
// SUDO_USER — buildSystemctlCommandForProd already handled it via runuser,
// though for ScopeSystem the runuser path doesn't kick in).
func needsSudo(scope Scope) bool {
	if scope != ScopeSystem {
		return false
	}
	return os.Geteuid() != 0
}

// userSystemdSocketPath returns the private socket path for the user
// manager of the given uid. Exposed as a helper so tests can compute the
// same path the production code polls.
func userSystemdSocketPath(uid string) string {
	return "/run/user/" + uid + "/systemd/private"
}

// waitForUserSystemd polls the user-systemd private socket until it
// appears or timeout elapses. enableLinger triggers the user manager
// asynchronously; without this wait, daemon-reload fired immediately
// after can race the manager coming up and exit 1 with no diagnostic.
func waitForUserSystemd(uid string, timeout time.Duration) error {
	socket := userSystemdSocketPath(uid)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(socket); err == nil && !st.IsDir() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", socket)
}

// waitForUserSystemdReady probes the user manager via `systemctl --user
// is-system-running` until the call connects — i.e. returns any output
// (even "degraded"/"starting") without a connection-refused style error.
// The socket file being present only proves the manager is accepting
// connections; on a freshly-linger-started manager the initial unit
// directory scan may still be in flight, and a daemon-reload issued in
// that window returns exit 0 without actually loading units. That race
// is what produces the misleading "Unit file default.target.wants/
// ccrouter.service does not exist" error at enable time.
//
// `run` is the command runner (real or fake). `scope` and `sudoUser`
// are passed through to buildSystemctlCommand so the probe uses the
// same runuser+env wrapper as the subsequent daemon-reload. If the
// wrapper cannot resolve sudoUser, the function falls back to the
// socket-poll (waitForUserSystemd) so the non-sudo path is unaffected.
func waitForUserSystemdReady(run func(string, ...string) ([]byte, error), scope Scope, uid, sudoUser string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		cmdName, cmdArgs, cmdErr := buildSystemctlCommand(scope, 0, sudoUser, func(string) (*user.User, error) {
			return user.Lookup(sudoUser)
		}, "is-system-running")
		if cmdErr != nil {
			return waitForUserSystemd(uid, timeout)
		}
		out, err := run(cmdName, cmdArgs...)
		// is-system-running exits non-zero with a short status word on
		// "degraded"/"maintenance"/"starting", but a connection failure
		// has empty stdout AND a non-nil err. We must reject "starting"
		// because the manager's initial coldplug (unit directory scan)
		// is still in flight — a daemon-reload issued during coldplug
		// gets merged into the coldplug job (systemd #11499) and becomes
		// a no-op, leaving the unit unloaded.
		status := strings.TrimSpace(string(out))
		if status != "" && status != "starting" {
			return nil // running, degraded, maintenance — coldplug done
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("user-systemd did not respond to is-system-running within %s (last error: %v)", timeout, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
// waitForUnitLoaded ensures a unit is actually present in the systemd
// manager's database before we attempt enable. It fires daemon-reload
// once (synchronous on systemd 245+ -- exit 0 means scan is complete),
// then polls LoadState without re-issuing reload to avoid the reload-merge
// antipattern (systemd #11499). If the poll exhausts its budget, a single
// retry reload is attempted with a 1s gap.
//
// Fatal LoadState values (masked, bad-setting, error) surface immediately.
func waitForUnitLoaded(
	run func(string, ...string) ([]byte, error),
	scope Scope, euid int, sudoUser string,
	serviceName string, timeout time.Duration,
) error {
	buildCmd := func(args ...string) (string, []string, error) {
		return buildSystemctlCommand(scope, euid, sudoUser, user.Lookup, args...)
	}

	// Phase 1: daemon-reload (once).
	reloadName, reloadArgs, err := buildCmd("daemon-reload")
	if err != nil {
		return err
	}
	reloadAction := fmt.Sprintf("%s %s", reloadName, strings.Join(reloadArgs, " "))
	if out, err := run(reloadName, reloadArgs...); err != nil {
		return wrapRunnerErr(reloadAction, out, err)
	}

	// Phase 2: poll LoadState at 500ms intervals.
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		showName, showArgs, err := buildCmd("show", serviceName, "--property=LoadState")
		if err != nil {
			return err
		}
		out, err := run(showName, showArgs...)
		ls := parseLoadState(out, err)
		switch ls {
		case "loaded":
			return nil
		case "masked":
			return fmt.Errorf("unit %s is masked -- run `systemctl --user unmask %s` first", serviceName, serviceName)
		case "bad-setting", "error":
			detail := fetchLoadError(run, buildCmd, serviceName)
			return fmt.Errorf("unit %s failed to load (%s)%s", serviceName, ls, detail)
		}
		time.Sleep(pollInterval)
	}

	// Phase 3: one retry reload with breathing room.
	if out, err := run(reloadName, reloadArgs...); err != nil {
		return wrapRunnerErr(reloadAction, out, err)
	}
	time.Sleep(1 * time.Second)
	showName, showArgs, _ := buildCmd("show", serviceName, "--property=LoadState")
	out, _ := run(showName, showArgs...)
	ls := parseLoadState(out, nil)
	if ls == "loaded" {
		return nil
	}
	return fmt.Errorf("user-systemd did not load unit %s within %s -- run `systemctl --user daemon-reload` manually, then re-run setup", serviceName, timeout)
}

// parseLoadState extracts the LoadState value from systemctl show output.
func parseLoadState(out []byte, err error) string {
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if strings.HasPrefix(line, "LoadState=") {
		return strings.TrimPrefix(line, "LoadState=")
	}
	return line
}

// fetchLoadError retrieves the LoadError property for diagnostic messages.
func fetchLoadError(run func(string, ...string) ([]byte, error), buildCmd func(...string) (string, []string, error), serviceName string) string {
	name, args, err := buildCmd("show", serviceName, "--property=LoadError")
	if err != nil {
		return ""
	}
	out, err := run(name, args...)
	if err != nil {
		return ""
	}
	detail := strings.TrimSpace(string(out))
	if strings.HasPrefix(detail, "LoadError=") {
		detail = strings.TrimPrefix(detail, "LoadError=")
	}
	if detail != "" && detail != "(null)" {
		return ": " + detail
	}
	return ""
}


// uidForUser looks up a username and returns its uid string. Wraps
// user.Lookup so callers don't repeat the lookup that buildSystemctlCommand
// already does.
func uidForUser(name string) (string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return u.Uid, nil
}

// enableLinger runs `loginctl enable-linger <user>` so the user's
// user-systemd instance starts at boot and survives logout. Without
// this, a user-scope service installed over SSH stops when the session
// ends — and on reboot, never starts. Best-effort: failure is logged
// but not fatal because some minimal containers lack a polkit/loginctl
// setup that permits it.
//
// When the process isn't root, escalate via sudo (polkit may also work
// on some systems, but sudo is more universally available). Uses the
// same -n-then-interactive retry pattern as writeFileWithMode so a
// passwordless-sudo box completes without a prompt, while an interactive
// shell still gets a sudo prompt.
func (s SystemdInstaller) enableLinger(username string) error {
	args := []string{"enable-linger", username}
	var out []byte
	var err error
	if os.Geteuid() == 0 {
		out, err = s.runner()("loginctl", args...)
	} else {
		out, err = s.runner()("sudo", append([]string{"-n", "loginctl"}, args...)...)
		if err != nil {
			out, err = s.runner()("sudo", append([]string{"loginctl"}, args...)...)
		}
	}
	if err != nil {
		return wrapRunnerErr(fmt.Sprintf("loginctl enable-linger %s", username), out, err)
	}
	return nil
}

// Enable performs daemon-reload, enable, and start. Each is a separate
// call so partial failures produce an error message that identifies
// which step failed. For user scope under bare sudo, the systemctl
// invocation drops to the invoking user via runuser and enables linger
// so the service survives the SSH session.
func (s SystemdInstaller) Enable(opts InstallOptions, unitPath string) error {
	const serviceName = "ccrouter"

	isUserUnderSudo := systemctlUserUnderSudo(opts.Scope)

	// Enable linger for every user-scope install so the service survives
	// reboot. Without linger, the user-systemd instance only runs during
	// an active login session — the service dies at logout and never
	// starts on boot. Best-effort: warn but continue if it fails — the
	// service still runs for the current session, and the warning
	// surfaces the remediation command for the operator.
	if opts.Scope == ScopeUser {
		lingerUser := ""
		if isUserUnderSudo {
			lingerUser = os.Getenv("SUDO_USER")
		}
		if lingerUser == "" {
			if u, err := user.Current(); err == nil && u.Username != "" {
				lingerUser = u.Username
			}
		}
		if lingerUser != "" {
			if err := s.enableLinger(lingerUser); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: %v (service runs this session but won't survive reboot)\n", err)
			}
		}
	}

	if isUserUnderSudo {
		sudoUser := os.Getenv("SUDO_USER")
		// Readiness probe stays here — only under sudo does enable-linger
		// spawn a fresh user-systemd that we need to wait for. Non-sudo
		// user-scope runs against the already-running session user-systemd
		// (the SSH session started it).
		//
		// enable-linger returns immediately but the user manager starts
		// asynchronously. Probe is-system-running until the manager
		// actually answers (not just until the socket appears — a freshly
		// started manager can take a moment before it loads its unit
		// directory, and a daemon-reload issued in that window returns
		// exit 0 without scanning).
		if uid, err := uidForUser(sudoUser); err == nil {
			if err := waitForUserSystemdReady(s.runner(), opts.Scope, uid, sudoUser, 10*time.Second); err != nil {
				return fmt.Errorf("user-systemd did not come up after enable-linger: %w — try `loginctl enable-linger %s` manually, then re-run", err, sudoUser)
			}
		}
	}

	// daemon-reload + unit-load verification. For user scope under sudo
	// we verify the unit is actually loaded (via LoadState) to catch the
	// race where daemon-reload returns exit 0 but the unit was never
	// scanned (e.g. merged into coldplug on systemd 245+). The non-sudo
	// path keeps a single reload -- no race window there.
	if isUserUnderSudo {
		sudoUser := os.Getenv("SUDO_USER")
		if err := waitForUnitLoaded(s.runner(), opts.Scope, 0, sudoUser, serviceName, 15*time.Second); err != nil {
			return err
		}
	} else {
		// runSystemctlCmdEscalating handles the sudo wrapping for
		// non-root ScopeSystem installs (the failing "Interactive
		// authentication required" case). For ScopeUser and actual root
		// it calls systemctl directly with no escalation.
		if err := runSystemctlCmdEscalating(s.runner(), opts.Scope, "daemon-reload"); err != nil {
			return err
		}
	}

	if err := runSystemctlCmdEscalating(s.runner(), opts.Scope, "enable", "--now", serviceName); err != nil {
		return err
	}
	return nil
}

// VerifyActive polls `systemctl is-active` until the service reports
// "active" continuously for `verifyActiveHoldSeconds` with a stable
// NRestarts count. Enable() returns nil the moment systemd accepts the
// start transaction — but a unit with ExecStart that exits 1 will show
// "activating (auto-restart)" forever while restart-looping, and Enable
// has already declared victory. This method closes that gap: it waits
// through at least one RestartSec=5 cycle and, if the unit never
// reaches "active", captures the last 30 journal lines plus `systemctl
// status` so the operator sees the actual failure instead of having to
// know to dig it out themselves.
//
// Tolerant of a single restart-and-recover within the window: a service
// that crashes on the first ExecStart (e.g. freshly-created system
// user's supplementary group membership not yet visible to systemd's
// first fork) and then succeeds via Restart=always is considered
// healthy, because it sustains active with a stable NRestarts count.
// A genuine crash-loop — NRestarts increments more than
// `maxRestartsDuringWindow` times, or the service never sustains
// active — still fails.
//
// `timeout` should be >= RestartSec + a safety margin. The default call
// site uses 20s against RestartSec=5. The probe interval is 500ms.
//
// Returns nil on sustained active with stable restart count. Returns
// an error wrapping the journal output on timeout or crash-loop —
// "activating", "failed", "deactivating", or empty (unit not found /
// systemctl errored). The caller is expected to surface the wrapped
// journal text to the user and return non-zero.
//
// verifyActiveHoldSeconds is the duration `is-active` must continuously
// report "active" — with no NRestarts increment in between — before
// VerifyActive declares the service healthy. Defaults to 3s so a
// Type=simple unit that forks, reports active, then crashes within one
// RestartSec=5 cycle is caught by the hold rather than falsely
// declared healthy on the first sample. Exposed as a var (not a const)
// so tests can shrink it to keep the suite fast.
var verifyActiveHoldSeconds = 3 * time.Second

// maxRestartsDuringWindow is the number of NRestarts increments
// tolerated within a single VerifyActive window. A single
// restart-and-recover (e.g. first-ExecStart permission issues resolved
// by group-cache refresh on the retry) is benign; two or more indicates
// a real crash-loop and must fail the gate. Exposed as a var so tests
// can manipulate it.
var maxRestartsDuringWindow = 1

func (s SystemdInstaller) VerifyActive(opts InstallOptions, timeout time.Duration) error {
	const serviceName = "ccrouter"
	const pollInterval = 500 * time.Millisecond

	deadline := time.Now().Add(timeout)
	// prevRestarts tracks the last-seen NRestarts value so we can detect
	// increments (each increment = one systemd-triggered restart).
	// -1 sentinel so the first sample never counts as an increment.
	prevRestarts := -1
	restartsDuringWindow := 0
	var holdStart time.Time
	for time.Now().Before(deadline) {
		nRaw := strings.TrimSpace(string(s.nRestarts(opts, serviceName)))
		curRestarts := -1
		if n, err := strconv.Atoi(nRaw); err == nil {
			curRestarts = n
		}

		// Detect NRestarts increments regardless of current state — a
		// crash-and-recover will tick NRestarts even while we briefly
		// observe "active" on the next sample.
		if prevRestarts >= 0 && curRestarts >= 0 && curRestarts > prevRestarts {
			restartsDuringWindow += curRestarts - prevRestarts
			// Any restart invalidates the sustained-active window: a
			// fresh crash just happened, so any hold we were building
			// is no longer "sustained". Require a new hold to start.
			holdStart = time.Time{}
			if restartsDuringWindow > maxRestartsDuringWindow {
				break // genuine crash-loop — surface diagnostics
			}
		}
		if curRestarts >= 0 {
			prevRestarts = curRestarts
		}

		state := s.isActiveState(opts, serviceName)
		if state == "active" {
			if holdStart.IsZero() {
				holdStart = time.Now()
			}
			if time.Since(holdStart) >= verifyActiveHoldSeconds {
				return nil // sustained active for the full hold with no restarts in between
			}
		} else {
			// Left "active" (or never reached it) — reset the hold. The
			// service may still recover via Restart=always, so keep
			// polling until deadline unless we exceed the restart cap.
			holdStart = time.Time{}
		}
		time.Sleep(pollInterval)
	}

	// Timed out or broke out — capture diagnostics. journalctl and
	// `systemctl status` both need the same sudo escalation rules as
	// systemctl itself for system-scope installs driven by a non-root
	// user. We don't have a dedicated helper for journalctl, so build
	// it inline mirroring runSystemctlCmdEscalating's sudo -n then
	// interactive pattern.
	journalOut := s.runCaptureEscalating(opts.Scope, "journalctl", "-u", serviceName, "-n", "30", "--no-pager")
	statusName, statusArgs, _ := buildSystemctlCommandForProd(opts.Scope, "status", serviceName, "--no-pager", "--full")
	statusOut, _ := s.runner()(statusName, statusArgs...)

	return fmt.Errorf("service did not reach a sustained active state within %s — last journal lines:\n%s\n\nsystemctl status:\n%s",
		timeout,
		strings.TrimSpace(string(journalOut)),
		strings.TrimSpace(string(statusOut)))
}

// isActiveState returns the trimmed output of `systemctl is-active
// ccrouter` (e.g. "active", "activating", "failed"). Empty string means
// the unit was not found or systemctl errored.
func (s SystemdInstaller) isActiveState(opts InstallOptions, serviceName string) string {
	cmdName, cmdArgs, err := buildSystemctlCommandForProd(opts.Scope, "is-active", serviceName)
	if err != nil {
		return ""
	}
	out, _ := s.runner()(cmdName, cmdArgs...)
	return strings.TrimSpace(string(out))
}

// nRestarts returns the trimmed value of the unit's NRestarts property
// (number of times systemd has restarted the unit since it was last
// enabled). Empty string means the property could not be read.
func (s SystemdInstaller) nRestarts(opts InstallOptions, serviceName string) []byte {
	cmdName, cmdArgs, err := buildSystemctlCommandForProd(opts.Scope, "show", serviceName, "-p", "NRestarts", "--value")
	if err != nil {
		return nil
	}
	out, _ := s.runner()(cmdName, cmdArgs...)
	return out
}

// runCaptureEscalating runs an arbitrary diagnostic command (journalctl,
// systemctl status) with the same sudo escalation rules as
// runSystemctlCmdEscalating. For ScopeSystem driven by a non-root user,
// tries `sudo -n` then interactive sudo. For ScopeUser and actual-root,
// runs directly. Returns combined stdout+stderr. Errors are tolerated —
// the caller (VerifyActive) is building a diagnostic message, not making
// a control-flow decision, so partial output is better than failing fast.
func (s SystemdInstaller) runCaptureEscalating(scope Scope, name string, args ...string) []byte {
	if !needsSudo(scope) {
		out, _ := s.runner()(name, args...)
		return out
	}
	sudoArgs := append([]string{"-n", name}, args...)
	if out, err := s.runner()("sudo", sudoArgs...); err == nil {
		return out
	}
	sudoArgs = append([]string{name}, args...)
	out, _ := s.runner()("sudo", sudoArgs...)
	return out
}

// Uninstall stops, disables, and removes the unit file. It leaves the
// service user and data directories intact — removing users is a
// privileged decision that belongs to the operator.
func (s SystemdInstaller) Uninstall(scope Scope) error {
	const serviceName = "ccrouter"
	// stop+disable are best-effort: a not-yet-installed service shouldn't
	// fail uninstall. We only error on the actual file removal.
	if cmdName, cmdArgs, err := buildSystemctlCommandForProd(scope, "stop", serviceName); err == nil {
		_, _ = s.runner()(cmdName, cmdArgs...)
	}
	if cmdName, cmdArgs, err := buildSystemctlCommandForProd(scope, "disable", serviceName); err == nil {
		_, _ = s.runner()(cmdName, cmdArgs...)
	}
	unitPath, err := unitPathFor(scope)
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	if cmdName, cmdArgs, err := buildSystemctlCommandForProd(scope, "daemon-reload"); err == nil {
		_, _ = s.runner()(cmdName, cmdArgs...)
	}
	return nil
}
