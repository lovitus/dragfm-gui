//go:build !windows

package endpoint

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSSHExecCombinesConcurrentStreamsWithoutDataRace(t *testing.T) {
	_, route := startIntegrationSSHServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote, err := DialSSH(ctx, "combined-output", "", route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	var combined bytes.Buffer
	command := `(i=0; while [ "$i" -lt 2000 ]; do printf 'stdout-line\n'; i=$((i+1)); done) &
(i=0; while [ "$i" -lt 2000 ]; do printf 'stderr-line\n' >&2; i=$((i+1)); done) &
wait`
	if err := remote.Exec(ctx, command, ExecOptions{Stdout: &combined, Stderr: &combined}); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"stdout-line\n", "stderr-line\n"} {
		if count := bytes.Count(combined.Bytes(), []byte(line)); count != 2000 {
			t.Fatalf("%q: got %d lines, expected 2000", line, count)
		}
	}
}
