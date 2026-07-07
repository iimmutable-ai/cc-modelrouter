package cli

import (
	"os/user"
	"path/filepath"
	"testing"
)

func TestWarnAboutBinaryLocation(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	homeBin := filepath.Join(u.HomeDir, "ccrouter")
	homeSubBin := filepath.Join(u.HomeDir, "go", "bin", "ccrouter")

	tests := []struct {
		name    string
		binPath string
		want    bool
	}{
		{"in tmp", "/tmp/ccrouter", true},
		{"in tmp subdir", "/tmp/build/ccrouter", true},
		{"in home", homeBin, true},
		{"in home subdir", homeSubBin, true},
		{"in usr local bin", "/usr/local/bin/ccrouter", false},
		{"in usr bin", "/usr/bin/ccrouter", false},
		{"in opt", "/opt/ccrouter/bin/ccrouter", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := warnAboutBinaryLocation(tt.binPath)
			if got != tt.want {
				t.Errorf("warnAboutBinaryLocation(%q) = %v, want %v", tt.binPath, got, tt.want)
			}
		})
	}
}

func TestSortedProviderNames(t *testing.T) {
	got := sortedProviderNames(nil)
	if len(got) != 0 {
		t.Errorf("sortedProviderNames(nil) = %v, want empty", got)
	}
}

func TestNewSetupServerCommand(t *testing.T) {
	cmd := NewSetupServerCommand()

	if cmd.Use != "server" {
		t.Errorf("expected Use %q, got %q", "server", cmd.Use)
	}

	f := cmd.Flags().Lookup("dry-run")
	if f == nil {
		t.Fatal("expected --dry-run flag")
	}
	if f.DefValue != "false" {
		t.Errorf("expected --dry-run default %q, got %q", "false", f.DefValue)
	}

	f2 := cmd.Flags().Lookup("config")
	if f2 == nil {
		t.Fatal("expected --config flag")
	}
}
