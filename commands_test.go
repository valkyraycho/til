package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func withTempDB(t *testing.T) {
	t.Helper()
	t.Setenv("TIL_DB", filepath.Join(t.TempDir(), "til.db"))
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fnErr := fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out), fnErr
}

func TestAddListSearchRmFlow(t *testing.T) {
	withTempDB(t)

	out, err := captureStdout(t, func() error {
		return cmdAdd([]string{"-t", "Docker,dns", "docker DNS fix details"})
	})
	if err != nil {
		t.Fatalf("cmdAdd: %v", err)
	}
	if !strings.Contains(out, "added #1") {
		t.Errorf("add output = %q", out)
	}

	out, err = captureStdout(t, func() error { return cmdList([]string{"-n", "10"}) })
	if err != nil {
		t.Fatalf("cmdList: %v", err)
	}
	if !strings.Contains(out, "docker DNS fix") || !strings.Contains(out, "[dns docker]") {
		t.Errorf("list output = %q", out)
	}

	out, err = captureStdout(t, func() error { return cmdSearch([]string{"docker"}) })
	if err != nil {
		t.Fatalf("cmdSearch: %v", err)
	}
	if !strings.Contains(out, "#1") {
		t.Errorf("search output = %q", out)
	}

	out, err = captureStdout(t, func() error { return cmdRm([]string{"1"}) })
	if err != nil {
		t.Fatalf("cmdRm: %v", err)
	}
	if !strings.Contains(out, "deleted #1") {
		t.Errorf("rm output = %q", out)
	}
	if err := cmdRm([]string{"1"}); err == nil {
		t.Errorf("second rm succeeded, want not-found error")
	}

	out, err = captureStdout(t, func() error { return cmdList(nil) })
	if err != nil {
		t.Fatalf("cmdList after rm: %v", err)
	}
	if !strings.Contains(out, "no notes found") {
		t.Errorf("list after rm = %q", out)
	}
}

func TestCmdSearchRequiresQuery(t *testing.T) {
	withTempDB(t)
	if err := cmdSearch(nil); err == nil {
		t.Errorf("cmdSearch with no query succeeded, want error")
	}
}

func TestCmdEditWithFakeEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell editor requires sh")
	}
	withTempDB(t)
	if _, err := captureStdout(t, func() error { return cmdAdd([]string{"original body"}) }); err != nil {
		t.Fatalf("cmdAdd: %v", err)
	}

	script := filepath.Join(t.TempDir(), "fake-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ' edited' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)

	out, err := captureStdout(t, func() error { return cmdEdit([]string{"1"}) })
	if err != nil {
		t.Fatalf("cmdEdit: %v", err)
	}
	if !strings.Contains(out, "updated #1") {
		t.Errorf("edit output = %q", out)
	}

	out, err = captureStdout(t, func() error { return cmdSearch([]string{"edited"}) })
	if err != nil {
		t.Fatalf("cmdSearch: %v", err)
	}
	if !strings.Contains(out, "#1") {
		t.Errorf("edited content not searchable: %q", out)
	}
}

func TestCmdEditEmptyResultAborts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell editor requires sh")
	}
	withTempDB(t)
	if _, err := captureStdout(t, func() error { return cmdAdd([]string{"precious content"}) }); err != nil {
		t.Fatalf("cmdAdd: %v", err)
	}

	script := filepath.Join(t.TempDir(), "clearing-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n: > \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)

	if err := cmdEdit([]string{"1"}); err == nil {
		t.Fatalf("cmdEdit with emptied file succeeded, want abort")
	}
	out, err := captureStdout(t, func() error { return cmdSearch([]string{"precious"}) })
	if err != nil {
		t.Fatalf("cmdSearch: %v", err)
	}
	if !strings.Contains(out, "#1") {
		t.Errorf("original body lost after aborted edit: %q", out)
	}
}

func TestCmdWebServesAndShutsDown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-signaling not supported on windows")
	}
	withTempDB(t)
	if _, err := captureStdout(t, func() error { return cmdAdd([]string{"web smoke note"}) }); err != nil {
		t.Fatalf("cmdAdd: %v", err)
	}

	const port = "47614"
	done := make(chan error, 1)
	go func() {
		_, err := captureStdout(t, func() error { return cmdWeb([]string{"-port", port}) })
		done <- err
	}()

	up := false
	for i := 0; i < 100; i++ {
		resp, err := http.Get("http://127.0.0.1:" + port + "/")
		if err == nil {
			resp.Body.Close()
			up = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !up {
		t.Fatal("cmdWeb never became reachable")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cmdWeb returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cmdWeb did not shut down within 5s of SIGINT")
	}
}

func TestNoteInputPipedStdin(t *testing.T) {
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if _, err := w.WriteString("piped content"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	w.Close()

	got, err := noteInput("")
	if err != nil {
		t.Fatalf("noteInput: %v", err)
	}
	if got != "piped content" {
		t.Errorf("noteInput = %q", got)
	}
}

func TestNoteInputInlineArg(t *testing.T) {
	got, err := noteInput("inline text wins")
	if err != nil {
		t.Fatalf("noteInput: %v", err)
	}
	if got != "inline text wins" {
		t.Errorf("noteInput = %q", got)
	}
}

func TestCmdRmInvalidID(t *testing.T) {
	withTempDB(t)
	for _, args := range [][]string{nil, {"abc"}, {"1", "2"}} {
		if err := cmdRm(args); err == nil {
			t.Errorf("cmdRm(%v) succeeded, want error", args)
		}
	}
}
