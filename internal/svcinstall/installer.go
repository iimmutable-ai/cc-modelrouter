// Package svcinstall installs ccrouter as a system service. The only
// implementation today is systemd on Linux; the Installer interface
// keeps the door open for launchd or openrc backends without churning
// call sites.
package svcinstall

// Scope selects where the service lives. ScopeSystem requires root and
// installs under /etc/systemd/system; ScopeUser installs under
// ~/.config/systemd/user and runs without elevated privileges.
type Scope int

const (
	ScopeSystem Scope = iota
	ScopeUser
)

func (s Scope) String() string {
	switch s {
	case ScopeSystem:
		return "system"
	case ScopeUser:
		return "user"
	}
	return "unknown"
}

// InstallOptions describes a single install invocation. The caller is
// responsible for resolving absolute paths before passing them in —
// relative paths would be baked verbatim into the unit file and resolved
// by systemd at service start time, which is rarely what anyone wants.
type InstallOptions struct {
	Scope      Scope
	BinaryPath string // absolute path to the ccrouter executable
	ConfigPath string // absolute path to config.json
	EnvFile    string // absolute path to a shell env file (EnvironmentFile=)
	HomeDir    string // WorkingDirectory; usually the service user's home
	DataDir    string // ReadWritePaths; where state (logs, instances, etc.) lives
	// User/Group are only meaningful for ScopeSystem. They identify the
	// unprivileged account the service drops to. Empty + ScopeSystem = the
	// installer will provision a default user named "ccrouter".
	User  string
	Group string
}

// InstallResult is what the installer hands back. UnitPath is always
// absolute.
type InstallResult struct {
	UnitPath string
	User     string
	Group    string
}

// Installer is the abstraction over service managers. Callers should
// program to this interface, not to *SystemdInstaller directly.
type Installer interface {
	// Available reports whether this service manager is usable on the
	// current host.
	Available() bool
	// RenderUnit produces the unit file body for inspection (dry-run
	// display) without writing anything to disk.
	RenderUnit(opts InstallOptions) (string, error)
	// Install writes the unit file and (for system scope) provisions
	// the runtime user. It does NOT enable or start the service — call
	// Enable explicitly so failures here are recoverable by rm + retry.
	Install(opts InstallOptions) (InstallResult, error)
	// Enable runs daemon-reload + enable + start (or the user-scope
	// equivalents).
	Enable(opts InstallOptions, unitPath string) error
	// Uninstall stops, disables, and removes the unit file. It does NOT
	// delete the service user or the data directory.
	Uninstall(scope Scope) error
}
