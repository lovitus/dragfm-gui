package endpoint

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestLocalCommandCancellationDoesNotWaitForChildPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	command := "sleep 30; printf should-not-run"
	if runtime.GOOS == "windows" {
		command = "ping -n 31 127.0.0.1 >nul & echo should-not-run"
	}
	var output bytes.Buffer
	started := time.Now()
	err := NewLocal().Exec(ctx, command, ExecOptions{Stdout: &output, Stderr: &output})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation error = %v", err)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("cancellation waited for child pipes")
	}
	if bytes.Contains(output.Bytes(), []byte("should-not-run")) {
		t.Fatal("cancelled command kept executing")
	}
}
