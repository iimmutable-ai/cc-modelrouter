// Package version exposes the build-identifiable version label for ccrouter.
//
// The label has the form "tag-buildhex" where:
//   - "tag" is branch-aware: "dev" on the dev-local branch, otherwise the latest
//     git tag reachable from HEAD (with a v0.1.0 fallback).
//   - "buildhex" is YY (decimal, kept) + hex(M) + hex(DD) + hex(H) + hex(mm).
//
// Tag and BuildTime are injected via ldflags at build time. When a binary is
// built with a plain `go build` (no ldflags), String() falls back to detecting
// the tag/build from the surrounding git repository at runtime; only when that
// also fails (e.g. binary copied outside any repo) does it return Fallback.
package version

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Tag is the version-tag component of the label, branch-aware:
//   - "dev" when built on the dev-local branch
//   - the latest git tag (e.g. "v0.1.0") on other branches
//
// Injected via ldflags. Empty when built without ldflags (plain `go build`).
var Tag = ""

// BuildTime is the build datetime as decimal YYMMDDHHmmss. Injected via ldflags.
// Empty when built without ldflags.
var BuildTime = ""

// Fallback is used when Tag/BuildTime are not injected and git detection fails.
const Fallback = "v0.1.0"

// detectFromGit is the runtime fallback when ldflags aren't injected.
// It's a package-level var so tests can stub it. The default implementation
// queries git for branch/tag info.
var detectFromGit = defaultDetectFromGit

// String returns "tag-buildhex" when both Tag and BuildTime are injected,
// otherwise tries git detection at runtime, finally returning Fallback.
func String() string {
	if Tag != "" && BuildTime != "" {
		return Tag + "-" + hexBuildTime(BuildTime)
	}
	// Plain `go build` path: detect at runtime from git environment
	tag, build := detectFromGit()
	if tag != "" && build != "" {
		return tag + "-" + hexBuildTime(build)
	}
	return Fallback
}

// hexBuildTime encodes a YYMMDDHHmmss build timestamp as
//
//	YY (decimal, kept) + hex(M) + hex(DD) + hex(H) + hex(mm)
//
// where each non-YY component is unpadded lowercase hex (e.g. month 6 -> "6",
// day 18 -> "12"). Seconds are dropped. On parse failure, returns the input
// unchanged so we never emit garbage.
//
// Example: "260618092600" -> "26" + "6" + "12" + "9" + "1a" -> "2661291a".
func hexBuildTime(s string) string {
	if len(s) != 12 {
		return s
	}
	yy, e1 := strconv.Atoi(s[0:2])
	mo, e2 := strconv.Atoi(s[2:4])
	dd, e3 := strconv.Atoi(s[4:6])
	hh, e4 := strconv.Atoi(s[6:8])
	mi, e5 := strconv.Atoi(s[8:10])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		return s
	}
	return fmt.Sprintf("%02d%s%s%s%s",
		yy,
		strconv.FormatInt(int64(mo), 16),
		strconv.FormatInt(int64(dd), 16),
		strconv.FormatInt(int64(hh), 16),
		strconv.FormatInt(int64(mi), 16),
	)
}

// defaultDetectFromGit determines tag and build time at runtime by invoking git.
// Used as a fallback when ldflags aren't injected (plain `go build`).
// Returns ("", "") if not in a git repo or git is unavailable, so callers
// fall through to Fallback.
func defaultDetectFromGit() (tag, build string) {
	branchOut, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", ""
	}
	branch := strings.TrimSpace(string(branchOut))

	if branch == "dev-local" {
		tag = "dev"
	} else {
		tagOut, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
		if err != nil {
			tag = "" // no tags reachable; signal fall-through to Fallback
		} else {
			tag = strings.TrimSpace(string(tagOut))
		}
	}

	build = time.Now().Format("060102150405") // YYMMDDHHmmss
	return tag, build
}
