package version

import (
	"strings"
	"testing"
)

// withVars sets Tag/BuildTime for the duration of a subtest and restores them
// afterwards, so tests do not leak state into each other or into other packages
// that import this one at runtime.
func withVars(t *testing.T, tag, build string, fn func()) {
	t.Helper()
	prevTag, prevBuild := Tag, BuildTime
	Tag, BuildTime = tag, build
	t.Cleanup(func() { Tag, BuildTime = prevTag, prevBuild })
	fn()
}

// withNoGitDetection stubs detectFromGit to return ("", "") so the test hits the
// final Fallback branch. Without this, the real git detection could run when
// Tag/BuildTime are empty (this test runs inside a real git repo).
func withNoGitDetection(t *testing.T) {
	t.Helper()
	orig := detectFromGit
	detectFromGit = func() (string, string) { return "", "" }
	t.Cleanup(func() { detectFromGit = orig })
}

func TestString_FallbackWhenBothEmpty(t *testing.T) {
	withNoGitDetection(t)
	withVars(t, "", "", func() {
		if got := String(); got != Fallback {
			t.Fatalf("String() = %q, want %q", got, Fallback)
		}
	})
}

func TestString_FallbackWhenBuildTimeEmpty(t *testing.T) {
	withNoGitDetection(t)
	withVars(t, "v0.1.0", "", func() {
		if got := String(); got != Fallback {
			t.Fatalf("String() = %q, want %q (need both Tag and BuildTime)", got, Fallback)
		}
	})
}

func TestString_FallbackWhenTagEmpty(t *testing.T) {
	withNoGitDetection(t)
	withVars(t, "", "260618092600", func() {
		if got := String(); got != Fallback {
			t.Fatalf("String() = %q, want %q (need both Tag and BuildTime)", got, Fallback)
		}
	})
}

func TestString_CombinesTagAndHexBuildTime(t *testing.T) {
	withVars(t, "v0.1.0", "260618092600", func() {
		if got := String(); got != "v0.1.0-2661291a" {
			t.Fatalf("String() = %q, want %q", got, "v0.1.0-2661291a")
		}
	})
}

func TestString_DevTag(t *testing.T) {
	withVars(t, "dev", "260618092600", func() {
		if got := String(); got != "dev-2661291a" {
			t.Fatalf("String() = %q, want %q", got, "dev-2661291a")
		}
	})
}

func TestHexBuildTime_KnownVectors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"release 2026-06-18 09:26", "260618092600", "2661291a"},
		// Cross-month boundary: 2026-12-31 23:59
		//   YY=26, M=12->c, DD=31->1f, H=23->17, mm=59->3b  =>  "26c1f173b"
		{"year-end 2026-12-31 23:59", "261231235900", "26c1f173b"},
		{"january 1st midnight", "260101000000", "261100"}, // M=1->1 DD=1->1 H=0->0 mm=0->0
		{"september 9th 09:09", "260909090900", "269999"}, // every non-YY component is single-hex-digit 9
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hexBuildTime(c.in); got != c.want {
				t.Fatalf("hexBuildTime(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHexBuildTime_InvalidInputReturnsRaw(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"too short", "2606180926"},    // 10 chars instead of 12
		{"too long", "26061809260000"}, // 14 chars
		{"empty", ""},
		{"non-numeric but right length", "garbage12gar"}, // len == 12 but not all digits
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hexBuildTime(c.in); got != c.in {
				t.Fatalf("hexBuildTime(%q) = %q, want input unchanged", c.in, got)
			}
		})
	}
}

func TestString_HexPartMatchesHelper(t *testing.T) {
	// Sanity: String() must prefix the helper output with "tag-".
	const tag = "v9.9.9"
	const build = "261231235900"
	withVars(t, tag, build, func() {
		got := String()
		want := tag + "-" + hexBuildTime(build)
		if got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
		if !strings.HasPrefix(got, tag+"-") {
			t.Fatalf("String() = %q, missing %q- prefix", got, tag)
		}
	})
}

// TestString_RuntimeFallbackStubbed verifies that when Tag/BuildTime are empty,
// String() uses the detectFromGit fallback output and formats it as tag-buildhex.
func TestString_RuntimeFallbackStubbed(t *testing.T) {
	orig := detectFromGit
	detectFromGit = func() (string, string) { return "dev", "260618092600" }
	t.Cleanup(func() { detectFromGit = orig })

	withVars(t, "", "", func() {
		got := String()
		want := "dev-2661291a"
		if got != want {
			t.Fatalf("String() = %q, want %q (runtime fallback)", got, want)
		}
	})
}

// TestDetectFromGit_RunsInRepo is a smoke test: the real defaultDetectFromGit
// must work inside this repo, returning a non-empty tag and a 12-char
// YYMMDDHHmmss build timestamp. Branch-agnostic — tag is "dev" on dev-local
// or the latest git tag (e.g. "v0.2.11") on other branches.
// If this fails outside a git checkout, the rest of the fallback chain still
// works (returns Fallback).
func TestDetectFromGit_RunsInRepo(t *testing.T) {
	tag, build := detectFromGit()
	if tag == "" {
		t.Fatalf("detectFromGit() tag empty — expected a value in a real git checkout")
	}
	if len(build) != 12 {
		t.Fatalf("detectFromGit() build = %q, want 12 chars (YYMMDDHHmmss)", build)
	}
}
