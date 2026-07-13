package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// withStubs snapshots the current test seam values, applies overrides, and
// restores them when the test ends. Keeps tests isolated.
func withStubs(t *testing.T, ipFn func(context.Context) (string, error), tty bool) {
	t.Helper()
	prevIP, prevTTYFn := detectPublicIPFn, isTTYFn
	detectPublicIPFn = ipFn
	isTTYFn = func() bool { return tty }
	t.Cleanup(func() {
		detectPublicIPFn = prevIP
		isTTYFn = prevTTYFn
	})
}

func TestDetectPublicIP_Live(t *testing.T) {
	if os.Getenv("CCROUTER_SKIP_NETWORK_TESTS") != "" {
		t.Skip("network tests disabled via CCROUTER_SKIP_NETWORK_TESTS")
	}

	ip, err := detectPublicIP(context.Background())
	if err != nil {
		t.Skipf("live detection failed (likely offline): %v", err)
	}
	if ip == "" {
		t.Fatal("expected non-empty IP")
	}
	// ipify returns bare IPv4 or IPv6 — sanity-check it has at least one dot
	// or colon so obviously broken responses (e.g. HTML) don't pass.
	if !strings.ContainsAny(ip, ".:") {
		t.Fatalf("returned IP %q does not look like an IP address", ip)
	}
	t.Logf("detected public IP: %s", ip)
}

func TestResolveServerAddress_URLFlagOverrides(t *testing.T) {
	// Detection must NOT be called: if it is, the test fails.
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when --url is set")
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{} // intentionally empty; must not be consumed
	got, err := resolveServerAddress("http://example.com:9000", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://example.com:9000" {
		t.Errorf("urlFlag must be returned verbatim, got %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output for --url path, got %q", out.String())
	}
}

func TestResolveServerAddress_IPFlagOverrides(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when --ip is set")
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "10.0.0.5", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://10.0.0.5:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_NonTTY_DefaultsLocal(t *testing.T) {
	called := false
	withStubs(t, func(context.Context) (string, error) {
		called = true
		return "", nil
	}, false) // non-TTY

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://localhost:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if called {
		t.Error("detectPublicIPFn must NOT be called on non-TTY path")
	}
	if !strings.Contains(out.String(), "local") {
		t.Errorf("expected summary line mentioning 'local', got %q", out.String())
	}
}

func TestResolveServerAddress_TTY_ChooseLocal(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when Local chosen")
		return "", nil
	}, true)

	// "1\n" picks Local; "\n" accepts default port.
	in := strings.NewReader("1\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://localhost:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChoosePublic_HTTP(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "203.0.113.42", nil
	}, true)

	// "3\n" → IP, "2\n" → HTTP scheme (default for IP), "y\n" → confirm, "\n" → default port (8081).
	in := strings.NewReader("3\n2\ny\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://203.0.113.42:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "203.0.113.42") {
		t.Errorf("expected detected IP in output, got %q", out.String())
	}
}

func TestResolveServerAddress_TTY_ChoosePublic_HTTP_Override(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "203.0.113.42", nil
	}, true)

	// "3\n" → IP, "2\n" → HTTP, "n\n" → override, "10.0.0.5\n" → new IP, "\n" → default port.
	in := strings.NewReader("3\n2\nn\n10.0.0.5\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://10.0.0.5:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChoosePublic_HTTPS(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "203.0.113.42", nil
	}, true)

	// "3\n" → IP, "1\n" → HTTPS scheme, "y\n" → confirm, "\n" → default port (443, omitted).
	in := strings.NewReader("3\n1\ny\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://203.0.113.42"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChoosePublic_DetectFails(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", errors.New("network unreachable")
	}, true)

	// "3\n" → IP, "2\n" → HTTP, detection fails → fallback to localhost,
	// "\n" accepts default port (8081 for http).
	in := strings.NewReader("3\n2\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("detection failure must not error, got: %v", err)
	}
	if want := "http://localhost:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "falling back to localhost") {
		t.Errorf("expected fallback message in output, got %q", out.String())
	}
}

func TestResolveServerAddress_TTY_PortOverride(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when Local chosen")
		return "", nil
	}, true)

	// "1\n" → Local, "9000\n" → custom port.
	in := strings.NewReader("1\n9000\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://localhost:9000"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- New tests for domain / scheme flag handling ---

func TestResolveServerAddress_DomainFlag(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when --domain is set")
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "", "api.example.com", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default scheme for domain is https; default port for https is 443 → omitted.
	if want := "https://api.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_DomainFlag_CustomPort(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when --domain is set")
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "", "api.example.com", "", 8443, true, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://api.example.com:8443"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_DomainFlag_StripsSchemePrefix(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	// User typed "https://api.example.com" at --domain; should strip the prefix.
	got, err := resolveServerAddress("", "", "https://api.example.com", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://api.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_IpFlag_HTTPS(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when --ip is set")
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "10.0.0.5", "", "https", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// scheme=https + default port 443 → omitted from URL.
	if want := "https://10.0.0.5"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_IpFlag_HTTPS_CustomPort(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", nil
	}, true)

	var out bytes.Buffer
	in := &bytes.Buffer{}
	got, err := resolveServerAddress("", "10.0.0.5", "", "https", 8443, true, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://10.0.0.5:8443"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChooseDomain_HTTPS(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		t.Fatal("detectPublicIPFn must not be called when Domain chosen")
		return "", nil
	}, true)

	// "2\n" → Domain, "\n" → accept HTTPS scheme default, "api.example.com\n" → domain,
	// "\n" → accept port default (443).
	in := strings.NewReader("2\n\napi.example.com\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://api.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChooseDomain_HTTP(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", nil
	}, true)

	// "2\n" → Domain, "2\n" → HTTP scheme, "api.example.com\n" → domain,
	// "\n" → accept port default (8081 for http).
	in := strings.NewReader("2\n2\napi.example.com\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://api.example.com:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_ChooseIP_HTTP(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "203.0.113.99", nil
	}, true)

	// "3\n" → IP, "\n" → accept HTTP scheme default (default 2 = HTTP for ip),
	// "y\n" → confirm detected IP, "\n" → accept port default (8081).
	in := strings.NewReader("3\n\ny\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://203.0.113.99:8081"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_TTY_DomainStripsSchemePrefix(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", nil
	}, true)

	// "2\n" → Domain, "\n" → accept HTTPS scheme, "https://api.example.com\n" →
	// user typed scheme prefix (should be stripped), "\n" → accept port default.
	in := strings.NewReader("2\n\nhttps://api.example.com\n\n")
	var out bytes.Buffer
	got, err := resolveServerAddress("", "", "", "", 8081, false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://api.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveServerAddress_PortExplicitOnNonTTY(t *testing.T) {
	withStubs(t, func(context.Context) (string, error) {
		return "", nil
	}, false) // non-TTY

	var out bytes.Buffer
	in := &bytes.Buffer{}
	// User passed --port 9999 explicitly → respect it on non-TTY path.
	got, err := resolveServerAddress("", "", "", "", 9999, true, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://localhost:9999"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
