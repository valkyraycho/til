package editor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"bare", "vi", []string{"vi"}},
		{"with flag", "code -w", []string{"code", "-w"}},
		{"quoted path with spaces", `"C:\Program Files\Microsoft VS Code\code.exe" -w`,
			[]string{`C:\Program Files\Microsoft VS Code\code.exe`, "-w"}},
		{"quoted mid-token", `emacs --eval "(setq x 1)"`, []string{"emacs", "--eval", "(setq x 1)"}},
		{"empty", "", nil},
		{"spaces only", "   ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitCommand(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("VISUAL", "visual-editor -a")
	t.Setenv("EDITOR", "plain-editor")
	if got := resolve(); got[0] != "visual-editor" || got[1] != "-a" {
		t.Errorf("VISUAL should win: got %q", got)
	}

	t.Setenv("VISUAL", "")
	if got := resolve(); got[0] != "plain-editor" {
		t.Errorf("EDITOR fallback: got %q", got)
	}

	t.Setenv("EDITOR", "")
	got := resolve()
	want := "vi"
	if runtime.GOOS == "windows" {
		want = "notepad"
	}
	if got[0] != want {
		t.Errorf("platform default: got %q, want %q", got, want)
	}
}

func TestEditRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell editor requires sh")
	}
	script := filepath.Join(t.TempDir(), "fake-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ' appended' >> \"$2\"\n"), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script+" --some-flag")

	got, err := Edit("original")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !strings.Contains(got, "original") || !strings.Contains(got, "appended") {
		t.Errorf("round trip content = %q", got)
	}
}

func TestEditNonZeroExitAborts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell editor requires sh")
	}
	script := filepath.Join(t.TempDir(), "failing-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)

	if _, err := Edit("content"); err == nil {
		t.Fatalf("Edit succeeded despite editor exit 1, want error")
	}
}
