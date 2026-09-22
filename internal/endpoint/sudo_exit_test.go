//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSudoRunClosedUnusedPasswordPipePreservesExitStatus(t *testing.T) {
	for _, code := range []int{0, 17} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			bin := t.TempDir()
			// The child deliberately never reads stdin, modelling NOPASSWD or
			// a rejected request. Oversized synthetic input forces a broken
			// pipe independently of parent/child scheduling. No real sudo runs.
			script := fmt.Sprintf("#!/bin/sh\nexec 0<&-\nexit %d\n", code)
			if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			sudo := &SudoLocal{local: NewLocal(), password: strings.Repeat("synthetic-only", 16384)}
			err := sudo.run(context.Background(), "true")
			if code == 0 && err != nil {
				t.Fatalf("successful NOPASSWD operation failed because stdin was unused: %v", err)
			}
			if code != 0 && (err == nil || !strings.Contains(err.Error(), "exit status 17")) {
				t.Fatalf("nonzero sudo exit was masked by the closed input pipe: %v", err)
			}
		})
	}
}

func TestSudoRunCancelledContextDoesNotBecomeSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sudo := &SudoLocal{local: NewLocal(), password: "synthetic-only"}
	if err := sudo.run(ctx, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation result: %v", err)
	}
}
