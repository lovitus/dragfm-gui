//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// Exercise the existing method entry without a GUI transferAccess context.
// Actual ordinary SSH accounts cannot read the source. The approved source
// helper and explicit peer root identity must stay in use through completion.
func TestHostedAgentMoveRetainsApprovedSourceView(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	rootKey, err := os.ReadFile(os.Getenv("DRAGFM_E2E_ROOT_KEY"))
	if err != nil {
		t.Fatal("explicit disposable root identity unavailable", err)
	}
	document.Keys = append(document.Keys, config.PrivateKey{ID: "move-root", Name: "move-root", PEM: string(rootKey)})
	for index := range document.Hosts {
		document.Hosts[index].RootKeyIDs = []string{"move-root"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		for _, pair := range []struct {
			ep   *endpoint.Remote
			path string
		}{{source, sourceRoot}, {target, targetRoot}} {
			// These are exact disposable fixture roots, never user directories.
			if err := pair.ep.Exec(cleanup, "sudo -n rm -rf -- "+shellQuote(pair.path), endpoint.ExecOptions{}); err != nil {
				t.Error("scoped fixture cleanup", err)
			}
		}
	})
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	defer app.Lock()
	app.document = document
	for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP} {
		t.Run(string(method), func(t *testing.T) {
			from, to := source.Join(sourceRoot, string(method)), target.Join(targetRoot, string(method))
			const contents = "the source's approved file view must survive the target completion branch"
			writeRemoteFile(t, ctx, source, from, contents)
			if err := source.Exec(ctx, "sudo -n chown root:root -- "+shellQuote(from)+" && sudo -n chmod 0600 -- "+shellQuote(from), endpoint.ExecOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := source.Exec(ctx, `test "$(id -u)" -ne 0 && ! test -r `+shellQuote(from), endpoint.ExecOptions{}); err != nil {
				t.Fatal("ordinary source account must not be able to read the fixture", err)
			}
			op := transfer.Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to, Move: true}
			err := app.runAgentMethod(ctx, op, mustPreflight(t, ctx, op), strategy.SourcePush, true, "", nil, nil, method)
			// Require publication before accepting the old failure as evidence;
			// broken setup/transport cannot masquerade as a completion regression.
			var copied bytes.Buffer
			if readErr := target.Exec(ctx, "sudo -n cat -- "+shellQuote(to), endpoint.ExecOptions{Stdout: &copied}); readErr != nil || copied.String() != contents {
				t.Fatalf("method did not reach actual target publication: %v; transfer: %v", readErr, err)
			}
			if err != nil {
				t.Fatalf("published move lost its approved source view: %v", err)
			}
			if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("verified protected source was not removed: %v", err)
			}
		})
	}
}
