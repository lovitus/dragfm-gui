//go:build integration && !windows

package webgui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
)

// Exercise the OLD public CleanupStale API against a real Linux SSH account.
// No executable helper is required, and no mocked endpoint authorizes deletion.
func TestHostedStaleCleanupPreservesLiveAndLegacyWorkspaces(t *testing.T) {
	source, _, _ := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	makeWorkspace := func(version int) string {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			t.Fatal(err)
		}
		nonce := hex.EncodeToString(random[:])
		directory := path.Join("/tmp", ".dragfm-"+nonce)
		if err := source.MkdirAll(ctx, directory, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = source.Remove(context.Background(), directory, true) })
		created := time.Now().Add(-48 * time.Hour)
		data, _ := json.Marshal(map[string]any{"version": version, "created": created, "nonce": nonce})
		writeRemoteFile(t, ctx, source, path.Join(directory, ".dragfm-owner-v1"), string(data))
		if err := source.Chmod(ctx, path.Join(directory, ".dragfm-owner-v1"), 0600); err != nil {
			t.Fatal(err)
		}
		writeRemoteFile(t, ctx, source, path.Join(directory, "archive.tar.gz"), "must survive active cleanup")
		if err := source.Chtimes(ctx, directory, created, created); err != nil {
			t.Fatal(err)
		}
		return directory
	}
	legacy, leased := makeWorkspace(1), makeWorkspace(2)
	input, feed := io.Pipe()
	output, stream := io.Pipe()
	defer input.Close()
	defer feed.Close()
	defer output.Close()
	defer stream.Close()
	done := make(chan error, 1)
	go func() {
		err := source.Exec(ctx, "exec bash --noprofile --norc -c "+shellQuote("exec 9< "+shellQuote(leased)+"; flock -s -n 9 || exit; printf 'locked\\n'; IFS= read -r unused || :"), endpoint.ExecOptions{Stdin: input, Stdout: stream})
		_ = stream.CloseWithError(err)
		done <- err
	}()
	stopRead := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()) })
	defer stopRead()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("real lease readiness: %q %v", line, err)
	}
	if err := remoteagent.CleanupStale(ctx, source, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{legacy, leased} {
		assertRemoteFile(t, ctx, source, path.Join(directory, "archive.tar.gz"), "must survive active cleanup")
	}
	_ = feed.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := remoteagent.CleanupStale(ctx, source, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Stat(ctx, leased); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expired unlocked workspace remains: %v", err)
	}
	assertRemoteFile(t, ctx, source, path.Join(legacy, "archive.tar.gz"), "must survive active cleanup")
	if _, err := source.List(ctx, "/tmp"); err != nil {
		t.Fatalf("cleanup closed the browsing connection: %v", err)
	}
}
