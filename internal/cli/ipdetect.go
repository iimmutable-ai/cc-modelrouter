package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// Test seams — override in tests to control detection / TTY detection.
var (
	detectPublicIPFn = detectPublicIP
	isTTYFn          = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }
)

// detectPublicIP queries an external echo service to learn the server's
// public-facing IPv4 address. It is ONLY called after the user explicitly
// selects the "Public" deployment type — never speculatively.
//
// 3s hard timeout. Returns ("", err) on timeout, non-200, or network failure.
//
// SECURITY: The detected IP is only written to stdout (prompting line) and to
// the generated settings file (both intentional user-facing outputs). It is
// never written to instance log files.
func detectPublicIP(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const endpoint = "https://api.ipify.org"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ipify returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty response from ipify")
	}
	return ip, nil
}

// resolveServerAddress applies the flag precedence table and returns the final
// base URL (e.g. "http://203.0.113.42:8081" or "http://localhost:8081").
//
// Precedence (first match wins):
//  1. urlFlag set  → use verbatim (no prompt, no detection)
//  2. ipFlag set   → http://<ip>:<port> (no prompt, no detection)
//  3. stdin not a TTY → localhost:<port>, no network call
//  4. stdin is a TTY  → prompt: [1] Local / [2] Public (detect)
//     - Local  → localhost
//     - Public → detectPublicIP with localhost fallback on failure,
//                then confirm/override prompt, then port prompt
//
// Detection failures are never fatal. The only error path is prompt I/O failure.
func resolveServerAddress(urlFlag, ipFlag string, port int, in io.Reader, out io.Writer) (string, error) {
	// 1. --url overrides everything.
	if urlFlag != "" {
		return urlFlag, nil
	}

	// 2. --ip skips prompt and detection (scripting / offline path).
	if ipFlag != "" {
		return fmt.Sprintf("http://%s:%d", ipFlag, port), nil
	}

	// 3. Non-TTY: default to localhost, no network call, one-line summary.
	if !isTTYFn() {
		fmt.Fprintf(out, "Using server IP: localhost (local)\n")
		return fmt.Sprintf("http://localhost:%d", port), nil
	}

	// 4. TTY: prompt deployment type.
	reader := bufio.NewReader(in)

	host, err := promptDeploymentType(reader, out)
	if err != nil {
		return "", err
	}

	// Prompt port (default = port flag value).
	finalPort, err := promptPort(reader, out, port)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("http://%s:%d", host, finalPort), nil
}

// promptDeploymentType asks the user to pick local vs public and returns the
// resolved host string. For "Public", detection failure falls back to localhost.
func promptDeploymentType(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		fmt.Fprintln(out, "Deployment type:")
		fmt.Fprintln(out, "  1) Local (localhost — same machine only)")
		fmt.Fprintln(out, "  2) Public  (remote server — detect public IP)")
		fmt.Fprint(out, "Select [1]: ")

		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read deployment choice: %w", err)
		}
		choice := strings.TrimSpace(line)

		switch choice {
		case "", "1":
			return "localhost", nil
		case "2":
			return promptPublicIP(reader, out)
		default:
			fmt.Fprintln(out, "Invalid choice; please enter 1 or 2.")
		}
	}
}

// promptPublicIP detects the public IP, then offers a confirm/override loop.
// Falls back to "localhost" if detection fails (informing the user).
func promptPublicIP(reader *bufio.Reader, out io.Writer) (string, error) {
	ip, err := detectPublicIPFn(context.Background())
	if err != nil {
		fmt.Fprintf(out, "Could not detect public IP (%v); falling back to localhost.\n", err)
		fmt.Fprintln(out, "Re-run with --ip <address> if you know the server IP.")
		return "localhost", nil
	}

	fmt.Fprintf(out, "Detected server IP: %s\n", ip)

	for {
		fmt.Fprint(out, "Use this IP? [Y/n]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read confirm: %w", err)
		}
		answer := strings.ToLower(strings.TrimSpace(line))

		switch answer {
		case "", "y", "yes":
			return ip, nil
		case "n", "no":
			fmt.Fprint(out, "Enter server IP: ")
			ovr, err := reader.ReadString('\n')
			if err != nil && ovr == "" {
				return "", fmt.Errorf("failed to read IP override: %w", err)
			}
			override := strings.TrimSpace(ovr)
			if override == "" {
				fmt.Fprintln(out, "Empty override; using detected IP.")
				return ip, nil
			}
			return override, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

// promptPort asks for the port, defaulting to the provided value on empty input.
func promptPort(reader *bufio.Reader, out io.Writer, defaultPort int) (int, error) {
	fmt.Fprintf(out, "Port [%d]: ", defaultPort)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return 0, fmt.Errorf("failed to read port: %w", err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return defaultPort, nil
	}

	port, err := parseIntPort(trimmed)
	if err != nil {
		fmt.Fprintf(out, "Invalid port %q; using default %d.\n", trimmed, defaultPort)
		return defaultPort, nil
	}
	return port, nil
}

// parseIntPort validates a port string in the legal range.
func parseIntPort(s string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(s, "%d", &port); err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}

// isLocalHostURL reports whether rawURL's host is a loopback address.
// It is used to decide whether gen settings should emit the non-local
// deployment warning (server must bind 0.0.0.0, firewalls must allow the port).
func isLocalHostURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
