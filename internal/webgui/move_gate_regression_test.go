//go:build linux

package webgui

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// The fixture really authenticates SSH, transfers via pkg/sftp's real file
// server (which does not advertise fsync), and executes OS commands. Faults
// live in isolated stat/sync executables, not a replacement Endpoint/transfer.
func moveGateRemote(t *testing.T, toolsDir string) *endpoint.Remote {
	t.Helper()
	run := func(channel ssh.Channel, command string) int {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "PATH=") {
				child.Env = append(child.Env, value)
			}
		}
		child.Env = append(child.Env, "PATH="+toolsDir+":/usr/bin:/bin")
		child.Stdin, child.Stdout, child.Stderr = channel, channel, channel.Stderr()
		if err := child.Run(); err != nil {
			var status *exec.ExitError
			if errors.As(err, &status) {
				return status.ExitCode()
			}
			return 255
		}
		return 0
	}
	address, pin := passwordRouteFixture(t, "move-gate-fixture", nil, run)
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote, err := endpoint.DialSSH(ctx, "move-gate", "", connector.Route{Timeout: 5 * time.Second, Hops: []connector.Hop{{
		Host: host, Port: port, User: "tester", Credentials: connector.Credentials{Password: "move-gate-fixture"},
		HostKey: connector.HostKeyPolicy{PinnedSHA256: pin},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	return remote
}

func TestSFTPControllerMoveRequiresFinalDurability(t *testing.T) {
	for _, mode := range []string{"supported", "missing-command", "file-failure", "publication-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			toolsDir := filepath.Join(root, "tools")
			if err := os.Mkdir(toolsDir, 0700); err != nil {
				t.Fatal(err)
			}
			// With operands, GNU sync performs actual fsync. Fail only the
			// chosen filesystem operation, while SFTP/hash/stat remain usable.
			script := "#!/bin/sh\nset -eu\n"
			switch mode {
			case "missing-command":
				script += "printf 'fixture sync capability unavailable\\n' >&2; exit 127\n"
			case "file-failure", "publication-failure":
				suffix := "/published/data"
				if mode == "publication-failure" {
					suffix = "/target-parent"
				}
				script += "for arg do case \"$arg\" in *" + suffix + ") printf 'fixture final sync failed\\n' >&2; exit 74;; esac; done\n"
			}
			script += "exec /usr/bin/sync \"$@\"\n"
			if err := os.WriteFile(filepath.Join(toolsDir, "sync"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "source")
			target := filepath.Join(root, "target-parent", "published")
			if err := os.Mkdir(source, 0750); err != nil {
				t.Fatal(err)
			}
			const contents = "the source must survive missing final durability"
			if err := os.WriteFile(filepath.Join(source, "data"), []byte(contents), 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("data", filepath.Join(source, "link")); err != nil {
				t.Fatal(err)
			}
			stamp := time.Unix(1_600_000_000, 0)
			if err := os.Chtimes(source, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			remote := moveGateRemote(t, toolsDir)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := transfer.Run(ctx, transfer.Operation{Source: endpoint.NewLocal(), Destination: remote, SourcePath: source, TargetPath: target, Move: true})
			if mode == "supported" {
				if err != nil || !result.Moved {
					t.Fatalf("actual SFTP plus equivalent sync did not move: %+v %v", result, err)
				}
				if _, err := os.Stat(source); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("confirmed move retained source: %v", err)
				}
			} else {
				if err == nil || transfer.Retryable(err) || !strings.Contains(err.Error(), "已复制但未移动") {
					t.Fatalf("missing final durability did not stop deletion: %+v %v", result, err)
				}
				data, readErr := os.ReadFile(filepath.Join(source, "data"))
				if readErr != nil || string(data) != contents {
					t.Fatalf("source lost after sync failure: %q %v", data, readErr)
				}
			}
			data, readErr := os.ReadFile(filepath.Join(target, "data"))
			if readErr != nil || string(data) != contents {
				t.Fatalf("target was not published before final sync: %q %v", data, readErr)
			}
			info, statErr := os.Stat(target)
			if statErr != nil || info.Mode().Perm() != 0750 || !info.ModTime().Equal(stamp) {
				t.Fatalf("final directory metadata not restored: %v %v", info, statErr)
			}
			if link, err := os.Readlink(filepath.Join(target, "link")); err != nil || link != "data" {
				t.Fatalf("target symlink changed: %q %v", link, err)
			}
		})
	}
}

func TestSSHIdentityLookupFailureAfterCopyKeepsSource(t *testing.T) {
	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	if err := os.Mkdir(toolsDir, 0700); err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	const contents = "same content and timestamps, different inode"
	if err := os.WriteFile(source, []byte(contents), 0640); err != nil {
		t.Fatal(err)
	}
	remote := moveGateRemote(t, toolsDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before, err := transfer.Snapshot(ctx, remote, source, true)
	if err != nil || before.RootInode == 0 {
		t.Fatalf("initial SSH identity unavailable: %+v %v", before, err)
	}
	op := transfer.Operation{Source: remote, Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target}
	if _, err := transfer.Run(ctx, op); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, []byte(contents), before.Items[0].Mode.Perm()); err != nil {
		t.Fatal(err)
	}
	stamp := before.Items[0].ModifiedTime()
	if err := os.Chtimes(replacement, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, source); err != nil {
		t.Fatal(err)
	}
	device, inode, err := remote.FileVersion(ctx, source)
	if err != nil || (device == before.RootDevice && inode == before.RootInode) {
		t.Fatalf("fixture did not replace real SSH source identity: %d/%d %v", device, inode, err)
	}
	// This is a real failing remote stat executable, including both GNU/BSD
	// and stdout-to-SFTP fallback attempts. SFTP file reads remain unchanged.
	if err := os.WriteFile(filepath.Join(toolsDir, "stat"), []byte("#!/bin/sh\nprintf 'fixture identity lookup failure\\n' >&2\nexit 74\n"), 0700); err != nil {
		t.Fatal(err)
	}
	op.Move = true
	if err := transfer.FinishMove(ctx, op, before); err == nil || transfer.Retryable(err) || !strings.Contains(err.Error(), "已复制但未移动") {
		t.Fatalf("failed SSH identity query did not preserve source: %v", err)
	}
	for _, file := range []string{source, target} {
		data, err := os.ReadFile(file)
		if err != nil || string(data) != contents {
			t.Fatalf("source/copy changed: %q %v", data, err)
		}
	}
}

func TestAcceleratedMoveKeepsSourceOnSyncFailure(t *testing.T) {
	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	if err := os.Mkdir(toolsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "sync"), []byte("#!/bin/sh\nprintf 'fixture final sync failed\\n' >&2\nexit 74\n"), 0700); err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	const contents = "accelerated copy does not authorize unsafe deletion"
	if err := os.WriteFile(source, []byte(contents), 0640); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	remote := moveGateRemote(t, toolsDir)
	op := transfer.Operation{Source: endpoint.NewLocal(), Destination: remote, SourcePath: source, TargetPath: target}
	before, err := transfer.Snapshot(ctx, op.Source, source, true)
	if err != nil {
		t.Fatal(err)
	}
	// This regression exercises the real completion gate after actual bytes
	// have been copied; the existing hosted transport matrix covers rsync/SCP.
	if _, err := transfer.Run(ctx, op); err != nil {
		t.Fatal(err)
	}
	op.Move = true
	if err := finishAcceleratedMove(ctx, op, before, "fixture-copy"); err == nil || transfer.Retryable(err) {
		t.Fatalf("accelerated completion deleted source without final durability: %v", err)
	}
	for _, path := range []string{source, target} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != contents {
			t.Fatalf("source/copy was lost or changed: %q %v", data, err)
		}
	}
}

func TestLocalSSHDirectoryOverlapIsRejectedBeforeQueuedCopy(t *testing.T) {
	for _, position := range []string{"same-root", "inside-source"} {
		t.Run(position, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "tree")
			if err := os.Mkdir(source, 0750); err != nil {
				t.Fatal(err)
			}
			const contents = "both panes refer to the original directory"
			file := filepath.Join(source, "data")
			if err := os.WriteFile(file, []byte(contents), 0640); err != nil {
				t.Fatal(err)
			}
			parent := root
			if position == "inside-source" {
				parent = filepath.Join(source, "child")
				if err := os.MkdirAll(filepath.Join(parent, "tree"), 0750); err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Unix(1_600_000_000, 0)
			for _, path := range []string{file, parent, source} {
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			remote := moveGateRemote(t, root)
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.panes[LeftPane] = &paneState{name: "本机", path: root, endpoint: endpoint.NewLocal()}
			app.panes[RightPane] = &paneState{name: "same-files-via-ssh", path: parent, endpoint: remote}
			app.mu.Unlock()
			events := observeCommands(t, app)
			preview, err := app.PrepareDrop(LeftPane, source, RightPane, parent)
			if err != nil {
				t.Fatal(err)
			}
			// Preview remains read-only. Actual overlap checks belong to the
			// confirmed task, not moving the pointer or cancelling the dialog.
			entries, err := os.ReadDir(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".dragfm-") {
					t.Fatal("preview wrote an overlap probe")
				}
			}
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview, Move: true, Overwrite: true})
			if err != nil {
				t.Fatal(err)
			}
			result := awaitCommandEvent(t, events, id, func(job outputJobWire) bool {
				return job.State == "succeeded" || job.State == "failed" || job.State == "cancelled"
			})
			data, err := os.ReadFile(file)
			if err != nil || string(data) != contents {
				t.Fatalf("source directory was deleted or changed: %q %v", data, err)
			}
			if position == "inside-source" {
				if _, err := os.Stat(filepath.Join(preview.TargetPath, "data")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("overlap was detected only after copying inside source: %v", err)
				}
			}
			if result.State != "failed" || !strings.Contains(result.Message, "重叠") {
				t.Fatalf("unsafe queued self-transfer was not rejected for overlap: %+v", result)
			}
			if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if strings.HasPrefix(entry.Name(), ".dragfm-") {
					t.Errorf("owned probe not cleaned: %s", filepath.Base(path))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
