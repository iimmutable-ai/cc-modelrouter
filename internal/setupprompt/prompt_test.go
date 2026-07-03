package setupprompt

import (
	"bytes"
	"strings"
	"testing"
)

func TestAskString_UsesDefaultOnEmpty(t *testing.T) {
	p := &Prompt{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	if got := p.AskString("Bind", "0.0.0.0"); got != "0.0.0.0" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestAskString_ReturnsInput(t *testing.T) {
	p := &Prompt{In: strings.NewReader("10.0.0.1\n"), Out: &bytes.Buffer{}}
	if got := p.AskString("Bind", "0.0.0.0"); got != "10.0.0.1" {
		t.Fatalf("expected input, got %q", got)
	}
}

func TestAskString_EOFReturnsDefault(t *testing.T) {
	p := &Prompt{In: strings.NewReader(""), Out: &bytes.Buffer{}}
	if got := p.AskString("Bind", "0.0.0.0"); got != "0.0.0.0" {
		t.Fatalf("expected default on EOF, got %q", got)
	}
}

func TestAskPassword_NonTTYReadsLine(t *testing.T) {
	p := &Prompt{In: strings.NewReader("sk-test-key\n"), Out: &bytes.Buffer{}}
	if got := p.AskPassword("API key"); got != "sk-test-key" {
		t.Fatalf("expected key, got %q", got)
	}
}

func TestAskChoice_NumberSelection(t *testing.T) {
	p := &Prompt{In: strings.NewReader("2\n"), Out: &bytes.Buffer{}}
	if got := p.AskChoice("TLS", []string{"Let's Encrypt", "Manual", "HTTP"}, 0); got != 1 {
		t.Fatalf("expected index 1, got %d", got)
	}
}

func TestAskChoice_EmptySelectsDefault(t *testing.T) {
	p := &Prompt{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	if got := p.AskChoice("Scope", []string{"system", "user"}, 0); got != 0 {
		t.Fatalf("expected default 0, got %d", got)
	}
}

func TestAskChoice_RepromptsOnInvalid(t *testing.T) {
	out := &bytes.Buffer{}
	p := &Prompt{In: strings.NewReader("nope\n9\n1\n"), Out: out}
	if got := p.AskChoice("TLS", []string{"A", "B"}, 0); got != 0 {
		t.Fatalf("expected 0 after retries, got %d", got)
	}
	if !strings.Contains(out.String(), "Invalid choice") {
		t.Fatalf("expected invalid-choice message in output, got: %s", out.String())
	}
}

func TestAskChoice_PrefixMatch(t *testing.T) {
	p := &Prompt{In: strings.NewReader("man\n"), Out: &bytes.Buffer{}}
	if got := p.AskChoice("TLS", []string{"Let's Encrypt", "Manual"}, 0); got != 1 {
		t.Fatalf("expected prefix match to index 1, got %d", got)
	}
}

func TestAskYesNo_DefaultYes(t *testing.T) {
	p := &Prompt{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	if !p.AskYesNo("ok?", true) {
		t.Fatal("expected default true")
	}
}

func TestAskYesNo_DefaultNo(t *testing.T) {
	p := &Prompt{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	if p.AskYesNo("ok?", false) {
		t.Fatal("expected default false")
	}
}

func TestAskYesNo_ExplicitNo(t *testing.T) {
	p := &Prompt{In: strings.NewReader("n\n"), Out: &bytes.Buffer{}}
	if p.AskYesNo("ok?", true) {
		t.Fatal("expected explicit no to override default yes")
	}
}

// TestPrompt_MultiLinePipedInput is a regression test for the bug where
// each Ask* call created a fresh bufio.Scanner over the same reader.
// When stdin is piped (or in tests using strings.NewReader), the kernel
// hands back many bytes per Read(); per-call scanners would eagerly
// buffer lines past the first, then get discarded — losing data.
// Fix: one persistent *bufio.Reader on the Prompt struct.
func TestPrompt_MultiLinePipedInput(t *testing.T) {
	// Three questions in a row, all answers in one input block.
	input := "10.0.0.1\n8443\ny\n"
	p := &Prompt{In: strings.NewReader(input), Out: &bytes.Buffer{}}

	got1 := p.AskString("Host", "0.0.0.0")
	got2 := p.AskString("Port", "8443")
	got3 := p.AskYesNo("ok?", false)

	if got1 != "10.0.0.1" {
		t.Errorf("Q1: got %q, want 10.0.0.1", got1)
	}
	if got2 != "8443" {
		t.Errorf("Q2: got %q, want 8443", got2)
	}
	if !got3 {
		t.Errorf("Q3: got false, want true")
	}
}

// TestPrompt_EOF_FlaggedAndIdempotent is a regression for the infinite-
// loop bug where AskString returned the default on EOF but the caller's
// loop kept asking the same question, reading the same EOF forever.
// Fix: readLine sets seenEOF on first EOF; subsequent calls short-circuit
// without touching the underlying reader. EOF() lets callers bail out.
func TestPrompt_EOF_FlaggedAndIdempotent(t *testing.T) {
	p := &Prompt{In: strings.NewReader(""), Out: &bytes.Buffer{}}

	if p.EOF() {
		t.Fatal("EOF should be false before any read")
	}
	got := p.AskString("Q", "default")
	if got != "default" {
		t.Errorf("on EOF, expected default; got %q", got)
	}
	if !p.EOF() {
		t.Fatal("EOF should be true after first EOF read")
	}
	// Subsequent calls must not block or spin.
	got2 := p.AskString("Q2", "still-default")
	if got2 != "still-default" {
		t.Errorf("after EOF, expected default; got %q", got2)
	}
}
