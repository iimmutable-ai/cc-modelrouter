package configwizard

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddToShellConfig(t *testing.T) {
	tests := []struct {
		name           string
		existingContent string
		providerName   string
		apiKey         string
		wantContains   []string
		wantNotContain []string
		wantLineCount  int // count of comment lines (should equal export line count)
	}{
		{
			name:           "fresh append no existing entries",
			existingContent: "export PATH=$HOME/bin:$PATH\n",
			providerName:   "openrouter",
			apiKey:         "sk-or-123",
			wantContains: []string{
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-123"`,
			},
			wantNotContain: []string{},
			wantLineCount:  1,
		},
		{
			name: "update existing export",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="old-key"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			apiKey:       "sk-or-new",
			wantContains: []string{
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-new"`,
			},
			wantNotContain: []string{
				"old-key",
			},
			wantLineCount: 1,
		},
		{
			name: "clean up corrupted file with multiple duplicates",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="key1"`,
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="key2"`,
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="key3"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			apiKey:       "sk-or-final",
			wantContains: []string{
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-final"`,
			},
			wantNotContain: []string{
				"key1",
				"key2",
				"key3",
			},
			wantLineCount: 1,
		},
		{
			name: "multiple providers coexist only target updated",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="or-key"`,
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			apiKey:       "or-new-key",
			wantContains: []string{
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="or-new-key"`,
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
			},
			wantNotContain: []string{
				"or-key",
			},
			wantLineCount: 1, // only openrouter lines
		},
		{
			name: "different provider comment preserved",
			existingContent: strings.Join([]string{
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			apiKey:       "or-key",
			wantContains: []string{
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="or-key"`,
			},
			wantNotContain: []string{},
			wantLineCount:  1, // openrouter lines only
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp RC file
			tmpDir := t.TempDir()
			rcPath := filepath.Join(tmpDir, ".zshrc")
			if err := os.WriteFile(rcPath, []byte(tt.existingContent), 0644); err != nil {
				t.Fatalf("failed to write temp rc file: %v", err)
			}

			sc := &ShellConfig{
				ShellPath:  "/bin/zsh",
				RCFilePath: rcPath,
			}

			if err := sc.AddToShellConfig(tt.providerName, tt.apiKey); err != nil {
				t.Fatalf("AddToShellConfig failed: %v", err)
			}

			result, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}

			resultStr := string(result)

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("result should contain %q\n--- got ---\n%s", want, resultStr)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(resultStr, notWant) {
					t.Errorf("result should NOT contain %q\n--- got ---\n%s", notWant, resultStr)
				}
			}

			// Count ccrouter lines for the target provider
			varName := GenerateEnvVarName(tt.providerName)
			comment := "# ccrouter - " + tt.providerName
			commentCount := strings.Count(resultStr, comment)
			exportCount := strings.Count(resultStr, "export "+varName+"=")
			if commentCount != tt.wantLineCount {
				t.Errorf("expected %d comment lines for %s, got %d\n--- got ---\n%s",
					tt.wantLineCount, tt.providerName, commentCount, resultStr)
			}
			if exportCount != tt.wantLineCount {
				t.Errorf("expected %d export lines for %s, got %d\n--- got ---\n%s",
					tt.wantLineCount, tt.providerName, exportCount, resultStr)
			}
		})
	}
}

func TestGenerateEnvVarName(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"openrouter", "CCROUTER_OPENROUTER_API_KEY"},
		{"bigmodel", "CCROUTER_BIGMODEL_API_KEY"},
		{"aliyun", "CCROUTER_ALIYUN_API_KEY"},
		{"anthropic", "CCROUTER_ANTHROPIC_API_KEY"},
		{"openai", "CCROUTER_OPENAI_API_KEY"},
		{"gemini", "CCROUTER_GEMINI_API_KEY"},
		{"minimax", "CCROUTER_MINIMAX_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := GenerateEnvVarName(tt.provider)
			if got != tt.want {
				t.Errorf("GenerateEnvVarName(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestRemoveFromShellConfig(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		providerName    string
		wantContains    []string
		wantNotContain  []string
	}{
		{
			name: "removes export for deleted provider",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-123"`,
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			wantContains: []string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
			},
			wantNotContain: []string{
				"# ccrouter - openrouter",
				"CCROUTER_OPENROUTER_API_KEY",
			},
		},
		{
			name: "removes multiple duplicates",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="key1"`,
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="key2"`,
				"",
			}, "\n"),
			providerName: "openrouter",
			wantContains: []string{
				"export PATH=$HOME/bin:$PATH",
			},
			wantNotContain: []string{
				"# ccrouter - openrouter",
				"CCROUTER_OPENROUTER_API_KEY",
				"key1",
				"key2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			rcPath := filepath.Join(tmpDir, ".zshrc")
			if err := os.WriteFile(rcPath, []byte(tt.existingContent), 0644); err != nil {
				t.Fatalf("failed to write temp rc file: %v", err)
			}

			sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: rcPath}
			if err := sc.RemoveFromShellConfig(tt.providerName); err != nil {
				t.Fatalf("RemoveFromShellConfig failed: %v", err)
			}

			result, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}
			resultStr := string(result)

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("result should contain %q\n--- got ---\n%s", want, resultStr)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(resultStr, notWant) {
					t.Errorf("result should NOT contain %q\n--- got ---\n%s", notWant, resultStr)
				}
			}
		})
	}
}

func TestRemoveFromShellConfig_no_match(t *testing.T) {
	existingContent := strings.Join([]string{
		"export PATH=$HOME/bin:$PATH",
		"# ccrouter - bigmodel",
		`export CCROUTER_BIGMODEL_API_KEY="bm-key"`,
		"",
	}, "\n")

	tmpDir := t.TempDir()
	rcPath := filepath.Join(tmpDir, ".zshrc")
	if err := os.WriteFile(rcPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("failed to write temp rc file: %v", err)
	}

	sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: rcPath}
	if err := sc.RemoveFromShellConfig("openrouter"); err != nil {
		t.Fatalf("RemoveFromShellConfig failed: %v", err)
	}

	result, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	resultStr := string(result)

	// Bigmodel should still be present
	if !strings.Contains(resultStr, "# ccrouter - bigmodel") {
		t.Error("bigmodel comment should still be present")
	}
	if !strings.Contains(resultStr, `CCROUTER_BIGMODEL_API_KEY="bm-key"`) {
		t.Error("bigmodel export should still be present")
	}
	if !strings.Contains(resultStr, "export PATH=$HOME/bin:$PATH") {
		t.Error("PATH export should still be present")
	}
}

func TestSourceNow(t *testing.T) {
	// Verify SourceNow uses os.Setenv (not broken exec.Command approach)
	sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: "/dev/null"}

	if err := sc.SourceNow("openrouter", "sk-test-123"); err != nil {
		t.Fatalf("SourceNow failed: %v", err)
	}

	got := os.Getenv("CCROUTER_OPENROUTER_API_KEY")
	if got != "sk-test-123" {
		t.Errorf("SourceNow did not set env var: got %q, want %q", got, "sk-test-123")
	}

	// Clean up
	os.Unsetenv("CCROUTER_OPENROUTER_API_KEY")
}

func TestSourceAllNow(t *testing.T) {
	sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: "/dev/null"}

	apiKeys := map[string]string{
		"openrouter": "sk-or-key",
		"bigmodel":   "bm-key-456",
		"gemini":     "gem-key",
	}
	sc.SourceAllNow(apiKeys)

	if got := os.Getenv("CCROUTER_OPENROUTER_API_KEY"); got != "sk-or-key" {
		t.Errorf("openrouter key not set: got %q", got)
	}
	if got := os.Getenv("CCROUTER_BIGMODEL_API_KEY"); got != "bm-key-456" {
		t.Errorf("bigmodel key not set: got %q", got)
	}
	if got := os.Getenv("CCROUTER_GEMINI_API_KEY"); got != "gem-key" {
		t.Errorf("gemini key not set: got %q", got)
	}

	// Clean up
	os.Unsetenv("CCROUTER_OPENROUTER_API_KEY")
	os.Unsetenv("CCROUTER_BIGMODEL_API_KEY")
	os.Unsetenv("CCROUTER_GEMINI_API_KEY")
}

func TestWriteEnvFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cc-modelrouter")
	envPath := filepath.Join(dir, "shell_env.sh")

	// Clean up before and after
	defer os.Remove(envPath)

	sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: "/dev/null"}
	apiKeys := map[string]string{
		"openrouter": "sk-or-12345",
		"bigmodel":   "bm-key-67890",
	}

	gotPath, err := sc.WriteEnvFile(apiKeys)
	if err != nil {
		t.Fatalf("WriteEnvFile failed: %v", err)
	}
	if gotPath != envPath {
		t.Errorf("WriteEnvFile returned wrong path: got %q, want %q", gotPath, envPath)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}
	content := string(data)

	// Verify header comments
	if !strings.Contains(content, "# Auto-generated by ccrouter config wizard") {
		t.Error("missing auto-generated header")
	}
	if !strings.Contains(content, "# Source this file to load API keys: source "+envPath) {
		t.Error("missing source hint in header")
	}

	// Verify export lines
	if !strings.Contains(content, `export CCROUTER_OPENROUTER_API_KEY="sk-or-12345"`) {
		t.Error("missing openrouter export")
	}
	if !strings.Contains(content, `export CCROUTER_BIGMODEL_API_KEY="bm-key-67890"`) {
		t.Error("missing bigmodel export")
	}

	// Verify no empty keys written
	if strings.Contains(content, `CCROUTER_EMPTY_API_KEY`) {
		t.Error("empty key should not be written")
	}

	// Test with empty key in map
	apiKeysWithEmpty := map[string]string{
		"openrouter": "sk-or-12345",
		"empty":      "",
	}
	_, err = sc.WriteEnvFile(apiKeysWithEmpty)
	if err != nil {
		t.Fatalf("WriteEnvFile with empty key failed: %v", err)
	}
	data2, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to re-read env file: %v", err)
	}
	content2 := string(data2)
	if strings.Contains(content2, "empty") {
		t.Error("provider with empty key should not appear in env file")
	}
}

// TestGenerateExportLine_EmitsProviderComment verifies that the export line
// generator emits a `# provider: <name>` comment so parseEnvExports can
// recover the original provider name (including case) from the file.
//
// This guards against the regression seen in v0.2.12 where a provider
// named `bigmodelKNDY` was written as `export CCROUTER_BIGMODELKNDY_API_KEY=`
// but read back as `bigmodelkndy`, causing setup to create a duplicate
// lowercased entry that crashed startup with "baseURL is required".
func TestGenerateExportLine_EmitsProviderComment(t *testing.T) {
	cases := []struct {
		name         string
		providerName string
		wantComment  string
	}{
		{"lowercase ascii", "openrouter", "# provider: openrouter"},
		{"mixed case", "bigmodelKNDY", "# provider: bigmodelKNDY"},
		{"uppercase", "GLM", "# provider: GLM"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GenerateExportLine(c.providerName, "fake-key")
			if !strings.Contains(got, c.wantComment) {
				t.Errorf("GenerateExportLine(%q) = %q; want it to contain %q", c.providerName, got, c.wantComment)
			}
		})
	}
}

// TestParseEnvExports_PreservesCaseFromComment verifies that when a
// `# provider: <name>` comment precedes an export line, the parser uses
// the comment's name verbatim — preserving the original casing.
func TestParseEnvExports_PreservesCaseFromComment(t *testing.T) {
	content := strings.Join([]string{
		"# Auto-generated by ccrouter config wizard",
		"# provider: bigmodelKNDY",
		`export CCROUTER_BIGMODELKNDY_API_KEY="key-one"`,
		"# provider: openrouter",
		`export CCROUTER_OPENROUTER_API_KEY="key-two"`,
		"",
	}, "\n")
	got := parseEnvExports(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries; got %d (%+v)", len(got), got)
	}
	if got["bigmodelKNDY"] != "key-one" {
		t.Errorf("expected bigmodelKNDY (mixed case) -> key-one; got map: %+v", got)
	}
	if _, ok := got["bigmodelkndy"]; ok {
		t.Errorf("must NOT contain lowercased key bigmodelkndy; got map: %+v", got)
	}
	if got["openrouter"] != "key-two" {
		t.Errorf("expected openrouter -> key-two; got map: %+v", got)
	}
}

// TestParseEnvExports_FallbackLowercaseWithoutComment verifies back-compat:
// an old shell_env.sh file without `# provider:` comments still parses, with
// provider names lowercased as before.
func TestParseEnvExports_FallbackLowercaseWithoutComment(t *testing.T) {
	content := strings.Join([]string{
		"# ccrouter - bigmodelKNDY (old format, no # provider: comment)",
		`export CCROUTER_BIGMODELKNDY_API_KEY="key-one"`,
		"",
	}, "\n")
	got := parseEnvExports(content)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry; got %d (%+v)", len(got), got)
	}
	if got["bigmodelkndy"] != "key-one" {
		t.Errorf("legacy fallback should lowercase to bigmodelkndy; got map: %+v", got)
	}
}

// TestParseEnvExports_CommentWithoutImmediateExportIsIgnored verifies that a
// `# provider:` comment that isn't followed by a matching export line does
// not leak into the output as a phantom entry.
func TestParseEnvExports_CommentWithoutImmediateExportIsIgnored(t *testing.T) {
	content := strings.Join([]string{
		"# provider: phantom",
		"# some other comment",
		`export CCROUTER_OPENROUTER_API_KEY="real-key"`,
		"",
	}, "\n")
	got := parseEnvExports(content)
	if _, ok := got["phantom"]; ok {
		t.Errorf("phantom entry from dangling comment must not appear; got map: %+v", got)
	}
	if got["openrouter"] != "real-key" {
		t.Errorf("expected openrouter -> real-key; got map: %+v", got)
	}
}

func TestParseEnvExports(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name: "standard preset names round-trip",
			content: strings.Join([]string{
				"# Auto-generated by ccrouter config wizard",
				"# Source this file to load API keys: source /home/u/.cc-modelrouter/shell_env.sh",
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-fake"`,
				"# ccrouter - bigmodel",
				`export CCROUTER_BIGMODEL_API_KEY="bm-fake"`,
				"",
			}, "\n"),
			want: map[string]string{
				"openrouter": "sk-or-fake",
				"bigmodel":   "bm-fake",
			},
		},
		{
			name:    "single provider no trailing newline",
			content: `export CCROUTER_GEMINI_API_KEY="gem-key"`,
			want:    map[string]string{"gemini": "gem-key"},
		},
		{
			name: "leading and trailing whitespace tolerated",
			content: strings.Join([]string{
				`  export CCROUTER_ANTHROPIC_API_KEY="ant-key"  `,
				`	export CCROUTER_OPENAI_API_KEY="oai-key"`,
				"",
			}, "\n"),
			want: map[string]string{
				"anthropic": "ant-key",
				"openai":    "oai-key",
			},
		},
		{
			name: "non-ccrouter exports skipped",
			content: strings.Join([]string{
				`export PATH=/usr/bin:$PATH`,
				`export HOME=/home/u`,
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-fake"`,
				"",
			}, "\n"),
			want: map[string]string{"openrouter": "sk-or-fake"},
		},
		{
			name: "empty value skipped",
			content: strings.Join([]string{
				`export CCROUTER_OPENROUTER_API_KEY=""`,
				`export CCROUTER_BIGMODEL_API_KEY="bm-real"`,
				"",
			}, "\n"),
			want: map[string]string{"bigmodel": "bm-real"},
		},
		{
			name: "missing quotes still parsed",
			content: strings.Join([]string{
				`export CCROUTER_OPENROUTER_API_KEY=sk-unquoted`,
				"",
			}, "\n"),
			want: map[string]string{"openrouter": "sk-unquoted"},
		},
		{
			name: "var without API_KEY suffix skipped",
			content: strings.Join([]string{
				`export CCROUTER_SOMETHING_ELSE="val"`,
				`export CCROUTER_OPENROUTER_API_KEY="sk-or-fake"`,
				"",
			}, "\n"),
			want: map[string]string{"openrouter": "sk-or-fake"},
		},
		{
			name:    "empty content yields empty map",
			content: "",
			want:    map[string]string{},
		},
		{
			name: "custom provider name lowercased with underscore",
			content: strings.Join([]string{
				`export CCROUTER_MY_PROVIDER_API_KEY="custom-key"`,
				"",
			}, "\n"),
			want: map[string]string{"my_provider": "custom-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEnvExports(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("parseEnvExports got %d entries, want %d (%+v)", len(got), len(tt.want), got)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseEnvExports[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cc-modelrouter")
	envPath := filepath.Join(dir, "shell_env.sh")

	// Snapshot any existing file so we can restore it after the test.
	var snapshot []byte
	if data, err := os.ReadFile(envPath); err == nil {
		snapshot = data
	}
	defer func() {
		if snapshot != nil {
			_ = os.WriteFile(envPath, snapshot, 0600)
		} else {
			os.Remove(envPath)
		}
	}()

	// Case 1: missing file returns empty map, nil error.
	os.Remove(envPath)
	sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: "/dev/null"}
	got, err := sc.LoadEnvFile()
	if err != nil {
		t.Fatalf("LoadEnvFile on missing file returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadEnvFile on missing file returned %d entries, want 0", len(got))
	}

	// Case 2: round-trip WriteEnvFile → LoadEnvFile.
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := map[string]string{
		"openrouter": "sk-or-12345",
		"bigmodel":   "bm-67890",
		"gemini":     "gem-key",
	}
	if _, err := sc.WriteEnvFile(seed); err != nil {
		t.Fatalf("WriteEnvFile failed: %v", err)
	}
	got, err = sc.LoadEnvFile()
	if err != nil {
		t.Fatalf("LoadEnvFile failed: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("LoadEnvFile got %d entries, want %d (%+v)", len(got), len(seed), got)
	}
	for name, key := range seed {
		if got[name] != key {
			t.Errorf("LoadEnvFile[%q] = %q, want %q", name, got[name], key)
		}
	}
}

func TestStripEnvVarPlaceholder(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "real key passes through unchanged",
			key:  "sk-real-key-value",
			want: "sk-real-key-value",
		},
		{
			name: "unset env var placeholder stripped to empty",
			key:  "${CCROUTER_ALICLOUD_API_KEY}",
			want: "",
		},
		{
			name: "self-referencing env var stripped",
			key:  "${CCROUTER_ALICLOUD_API_KEY}sk-actual-key",
			want: "sk-actual-key",
		},
		{
			name: "non-ccrouter env var not stripped",
			key:  "${SOME_OTHER_VAR}value",
			want: "${SOME_OTHER_VAR}value",
		},
		{
			name: "empty string stays empty",
			key:  "",
			want: "",
		},
		{
			name: "malformed no closing brace not stripped",
			key:  "${CCROUTER_OPENROUTER_API_KEY",
			want: "${CCROUTER_OPENROUTER_API_KEY",
		},
		{
			name: "placeholder only with extra content after",
			key:  "${CCROUTER_BIGMODEL_API_KEY}bm-real-123",
			want: "bm-real-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripEnvVarPlaceholder(tt.key)
			if got != tt.want {
				t.Errorf("stripEnvVarPlaceholder(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestSyncAllShellExports(t *testing.T) {
	const sentinelLine = "# ccrouter-env (auto-source API keys — managed by ccrouter config)"
	const sourceLine = `[ -f ~/.cc-modelrouter/shell_env.sh ] && source ~/.cc-modelrouter/shell_env.sh`

	tests := []struct {
		name            string
		existingContent string
		apiKeys         map[string]string
		wantContains    []string
		wantNotContain  []string
	}{
		{
			name: "full reconciliation strips per-provider exports and adds single source line",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				"# ccrouter - stale_provider",
				`export CCROUTER_STALE_PROVIDER_API_KEY="old-key"`,
				"# ccrouter - openrouter",
				`export CCROUTER_OPENROUTER_API_KEY="or-key"`,
				"",
			}, "\n"),
			apiKeys: map[string]string{
				"openrouter": "sk-or-new",
				"bigmodel":   "bm-real-key",
			},
			wantContains: []string{
				"export PATH=$HOME/bin:$PATH",
				sentinelLine,
				sourceLine,
			},
			wantNotContain: []string{
				"# ccrouter -",
				"export CCROUTER_",
				"stale_provider",
				"STALE_PROVIDER",
				"old-key",
				"or-key",
				"sk-or-new",
				"bm-real-key",
			},
		},
		{
			name: "re-running is idempotent — exactly one source block",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				sentinelLine,
				sourceLine,
				"",
			}, "\n"),
			apiKeys: map[string]string{
				"openrouter": "sk-or-final",
			},
			wantContains: []string{
				"export PATH=$HOME/bin:$PATH",
				sentinelLine,
				sourceLine,
			},
			wantNotContain: []string{
				"export CCROUTER_",
				"sk-or-final",
			},
		},
		{
			name: "legacy auto-source form is also stripped",
			existingContent: strings.Join([]string{
				"export PATH=$HOME/bin:$PATH",
				`[ -f ~/.cc-modelrouter/shell_env.sh ] && source ~/.cc-modelrouter/shell_env.sh`,
				"",
			}, "\n"),
			apiKeys:      map[string]string{"openrouter": "k"},
			wantContains: []string{sentinelLine, sourceLine},
		},
		{
			name:            "empty apiKeys still writes the source line (keys resolved at source time)",
			existingContent: "export PATH=$HOME/bin:$PATH\n# ccrouter - x\nexport CCROUTER_X_API_KEY=\"k\"\n",
			apiKeys:         map[string]string{},
			wantContains:    []string{sentinelLine, sourceLine},
			wantNotContain:  []string{"export CCROUTER_", "# ccrouter -"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			rcPath := filepath.Join(tmpDir, ".zshrc")
			if err := os.WriteFile(rcPath, []byte(tt.existingContent), 0644); err != nil {
				t.Fatalf("failed to write temp rc file: %v", err)
			}

			sc := &ShellConfig{ShellPath: "/bin/zsh", RCFilePath: rcPath}
			if err := sc.SyncAllShellExports(tt.apiKeys); err != nil {
				t.Fatalf("SyncAllShellExports failed: %v", err)
			}

			result, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}
			resultStr := string(result)

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("result should contain %q\n--- got ---\n%s", want, resultStr)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(resultStr, notWant) {
					t.Errorf("result should NOT contain %q\n--- got ---\n%s", notWant, resultStr)
				}
			}

			if c := strings.Count(resultStr, sourceLine); c != 1 {
				t.Errorf("expected exactly 1 source line, got %d\n--- got ---\n%s", c, resultStr)
			}
			if c := strings.Count(resultStr, sentinelLine); c != 1 {
				t.Errorf("expected exactly 1 sentinel comment, got %d\n--- got ---\n%s", c, resultStr)
			}
		})
	}
}

func TestEffectiveHomeDir(t *testing.T) {
	// Reference values.
	osHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir failed: %v", err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current failed: %v", err)
	}

	tests := []struct {
		name       string
		unsetFirst bool
		sudoUser   string
		want       string
	}{
		{
			name:       "SUDO_USER unset falls back to os.UserHomeDir",
			unsetFirst: true,
			want:       osHome,
		},
		{
			name:     "SUDO_USER set to current user resolves to current user's home",
			sudoUser: current.Username,
			want:     current.HomeDir,
		},
		{
			name:     "SUDO_USER set to nonexistent user falls back to os.UserHomeDir",
			sudoUser: "nonexistentuser_ccrouter_test_zzz",
			want:     osHome,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unsetFirst {
				t.Setenv("SUDO_USER", "")
			} else {
				t.Setenv("SUDO_USER", tt.sudoUser)
			}

			got, err := EffectiveHomeDir()
			if err != nil {
				t.Fatalf("EffectiveHomeDir returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("EffectiveHomeDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
