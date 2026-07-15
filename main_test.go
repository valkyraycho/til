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
