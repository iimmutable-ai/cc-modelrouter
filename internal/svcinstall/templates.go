package svcinstall

import (
	_ "embed"
	"text/template"
)

// unitTemplateText is the systemd unit body. The .User conditional gates
// the security-hardening directives (NoNewPrivileges, ProtectSystem, etc.)
// which are invalid in user-scope units.
//
//go:embed templates/ccrouter.service.tmpl
var unitTemplateText string

// unitTemplate is parsed once at package init. Template parse errors are
// programmer-only (a malformed .tmpl checked into the tree), so panicking
// here surfaces them at the first test run rather than at install time on
// a remote box.
var unitTemplate = template.Must(template.New("ccrouter.service").Parse(unitTemplateText))

// templateFields mirrors the template variables in ccrouter.service.tmpl.
// Exported field names match the {{.Name}} references exactly.
type templateFields struct {
	User       string
	Group      string
	BinaryPath string
	ConfigPath string
	EnvFile    string
	HomeDir    string
	DataDir    string
	Target     string // "multi-user.target" (system) or "default.target" (user)
}
