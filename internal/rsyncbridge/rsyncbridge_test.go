package rsyncbridge

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestRsyncServerArgumentsRejectsArbitraryCommands(t *testing.T) {
	valid, err := rsyncServerArguments([]string{"-l", "user", "dragfm", "rsync", "--server", "-logDtpre.iLsfxCIvu", ".", "/tmp/out"})
	if err != nil || len(valid) != 5 || valid[0] != "rsync" {
		t.Fatalf("valid command rejected: %#v %v", valid, err)
	}
	for _, arguments := range [][]string{{"dragfm", "sh", "-c", "id"}, {"dragfm", "rsync", "--daemon"}} {
		if _, err := rsyncServerArguments(arguments); err == nil {
			t.Fatalf("unsafe command accepted: %#v", arguments)
		}
	}
}

func TestShellQuoting(t *testing.T) {
	if got := joinShellWords([]string{"rsync", "a b", "x'y"}); got != "'rsync' 'a b' 'x'\\''y'" {
		t.Fatalf("unexpected quote: %s", got)
	}
}

func TestRunReturnsWhenRsyncExitsBeforeTransport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "rsync")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho early failure >&2\nexit 23\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	err := Run(ctx, &ssh.Client{}, Upload, "/source", "/target")
	if err == nil || !strings.Contains(err.Error(), "early failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatalf("early rsync failure was not observed promptly")
	}
}
