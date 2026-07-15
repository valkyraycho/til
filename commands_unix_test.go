//go:build unix

package main

import (
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestCmdWebServesAndShutsDown(t *testing.T) {
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
	for range 100 {
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
