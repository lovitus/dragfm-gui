//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestHostedSSHCommandCancellationLatency(t *testing.T) {
	source, _, _ := fixtureEndpoints(t)
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	root := remoteTempDir(t, ctx, source)
	defer source.Remove(context.Background(), root, true)
	pidPath := source.Join(root, "command.pid")
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- source.Exec(commandCtx, "echo $$ > "+shellQuote(pidPath)+"; sleep 30; touch "+shellQuote(source.Join(root, "must-not-exist")), endpoint.ExecOptions{})
	}()
	var pid string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var result bytes.Buffer
		if source.Exec(ctx, "cat -- "+shellQuote(pidPath), endpoint.ExecOptions{Stdout: &result}) == nil {
			pid = strings.TrimSpace(result.String())
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("remote command never started")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("command did not preserve cancellation: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("SSH cancellation exceeded four seconds")
	}
	if source.IsClosed() {
		t.Fatal("cooperative SSH cancellation unnecessarily closed the endpoint")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("SSH cancellation exceeded three seconds")
	}
	for deadline := time.Now().Add(2 * time.Second); ; {
		if source.Exec(ctx, "kill -0 "+shellQuote(pid), endpoint.ExecOptions{}) != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled remote command is still alive")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := source.Stat(ctx, source.Join(root, "must-not-exist")); err == nil {
		t.Fatal("cancelled command continued")
	}
	t.Log("real SSH cancellation is bounded; server process is gone; endpoint still usable")
}
