package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDBPathOverride(t *testing.T) {
	t.Setenv("TIL_DB", "/custom/place/notes.db")
	got, err := dbPath()
	if err != nil {
		t.Fatalf("dbPath: %v", err)
	}
	if got != "/custom/place/notes.db" {
		t.Errorf("dbPath = %q, want TIL_DB override", got)
	}
}

func TestDBPathDefault(t *testing.T) {
	t.Setenv("TIL_DB", "")
	got, err := dbPath()
	if err != nil {
		t.Fatalf("dbPath: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".til", "til.db")) {
		t.Errorf("dbPath = %q, want ~/.til/til.db", got)
	}
}

func TestRunDispatcher(t *testing.T) {
	t.Setenv("TIL_DB", filepath.Join(t.TempDir(), "til.db"))
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args", []string{"til"}, 2},
		{"help", []string{"til", "help"}, 0},
		{"unknown command", []string{"til", "bogus"}, 2},
		{"add", []string{"til", "add", "dispatcher note"}, 0},
		{"list", []string{"til", "list"}, 0},
		{"search hit", []string{"til", "search", "dispatcher"}, 0},
		{"rm missing entry", []string{"til", "rm", "999"}, 1},
		{"rm", []string{"til", "rm", "1"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseID(t *testing.T) {
	if _, err := parseID([]string{"12"}); err != nil {
		t.Errorf("parseID(12): %v", err)
	}
	for _, args := range [][]string{{}, {"a"}, {"1", "2"}} {
		if _, err := parseID(args); err == nil {
			t.Errorf("parseID(%v) succeeded, want error", args)
		}
	}
}
