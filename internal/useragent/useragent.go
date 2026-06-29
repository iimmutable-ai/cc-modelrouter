// Package useragent resolves the User-Agent header ccrouter sends to providers.
//
// The default mimics the @anthropic-ai/sdk User-Agent that Claude Code sends, so
// Anthropic-protocol provider front-ends (e.g. GLM's /api/anthropic) see traffic
// as if it originated from Claude Code itself. Operators can override the value
// via ServerConfig.UserAgent.
package useragent

import "strings"

// claudeCodeDefault is the User-Agent Claude Code 2.1.x sends on outbound
// requests to Anthropic-protocol providers. It is built from the
// @anthropic-ai/sdk version Claude Code bundles plus the Bun runtime it ships
// as. Sourced from the installed Claude Code binary's bundled strings.
const claudeCodeDefault = "@anthropic-ai/sdk/0.30.0 bun/1.3.13 darwin/arm64"

// Default returns the User-Agent string that mimics Claude Code.
func Default() string {
	return claudeCodeDefault
}

// Resolve returns the operator-configured User-Agent if set, otherwise the
// Claude Code default. A whitespace-only value is treated as unset.
func Resolve(cfgUserAgent string) string {
	if v := strings.TrimSpace(cfgUserAgent); v != "" {
		return v
	}
	return claudeCodeDefault
}
