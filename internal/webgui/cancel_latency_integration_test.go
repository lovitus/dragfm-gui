//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestHostedSSHCommandCancellationLatency(t *testing.T) {
	source, _, _ := fixtureEndpoints(t)
	for _, elevated := range []bool{false, true} {
		t.Run(fmt.Sprintf("sudo=%t", elevated), func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
			defer stop()
			root := remoteTempDir(t, ctx, source)
			defer source.Remove(context.Background(), root, true)
			commandCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			ready := make(chan string, 1)
			go func() {
				stream := bufio.NewReader(reader)
				line, _ := stream.ReadString('\n')
				ready <- strings.TrimSpace(line)
				_, _ = io.Copy(io.Discard, stream)
			}()
			command := "printf '%s\\n' \"$$\"; sleep 30; touch " + shellQuote(source.Join(root, "must-not-exist"))
			if elevated {
				// A single root child isolates the sudo monitor's signal relay.
				// The dual-protected queue case covers full root pipelines.
				command = "exec sudo -n /bin/sh -c " + shellQuote("printf '%s\\n' \"$$\"; exec sleep 30")
			}
			done := make(chan error, 1)
			go func() {
				defer writer.Close()
				done <- source.Exec(commandCtx, command, endpoint.ExecOptions{Stdout: writer})
			}()
			var pid string
			select {
			case pid = <-ready:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if number, err := strconv.Atoi(pid); err != nil || number <= 1 {
				t.Fatal("remote command did not report a valid PID over its stdout")
			}
			var identity bytes.Buffer
			if err := source.Exec(ctx, "ps -o uid=,pgid= -p "+pid, endpoint.ExecOptions{Stdout: &identity}); err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(identity.String())
			if len(fields) != 2 || (fields[0] == "0") != elevated {
				t.Fatal("command did not run under the requested actual privilege")
			}
			group, err := strconv.Atoi(fields[1])
			if err != nil || group <= 1 {
				t.Fatal("command did not have a valid owned SSH process group")
			}
			// Test failure must not leave this disposable command's process group
			// running. No process-name matching or unrelated group is used.
			exited := false
			t.Cleanup(func() {
				if !exited {
					_ = source.Exec(context.Background(), fmt.Sprintf("sudo -n kill -KILL -- -%d", group), endpoint.ExecOptions{})
				}
			})
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
			if source.Exec(ctx, "sudo -n kill -0 "+pid, endpoint.ExecOptions{}) == nil {
				t.Fatal("cancelled remote command is still alive after Exec returned")
			}
			exited = true
			if _, err := source.Stat(ctx, source.Join(root, "must-not-exist")); err == nil {
				t.Fatal("cancelled command continued")
			}
			t.Log("real SSH cancellation is bounded; server process is gone; endpoint still usable")
		})
	}
}
