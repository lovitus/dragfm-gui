//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

// Public connection/queue/lock/unlock path with genuine Linux ownership and
// sudo. Journal fields travel through JSON so the baseline compiles and fails
// for absent recovery, not for a new struct field/API missing from old code.
func TestHostedReconnectRecoversOnlyJournaledWorkspaces(t *testing.T) {
	source, _, document := fixtureEndpoints(t)
	for _, candidate := range []struct {
		name                                                                     string
		partial                                                                  string
		elevated, uploaderOwned, skip, intent, badHost, badMarker, replaceParent bool
	}{
		{name: "ordinary"}, {name: "write-ahead-intent", intent: true}, {name: "approved-root", elevated: true},
		{name: "approved-root-uploader-owned", elevated: true, uploaderOwned: true},
		{name: "declined-root", elevated: true, skip: true}, {name: "different-host", badHost: true},
		{name: "marker-replaced", badMarker: true}, {name: "parent-replaced", replaceParent: true},
		{name: "pinned-partial-file", partial: "file"}, {name: "pinned-partial-link", partial: "link"},
		{name: "approved-root-pinned-readonly-tree", partial: "tree", elevated: true}, {name: "partial-inode-replaced", partial: "replaced"},
		{name: "partial-not-pinned", partial: "unpinned"},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			parent := remoteTempDir(t, ctx, source) // /var/tmp: not covered by a /tmp-only scan.
			moved := parent + "-moved"
			t.Cleanup(func() {
				for _, directory := range []string{parent, moved} {
					_ = source.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(directory), endpoint.ExecOptions{})
				}
			})
			var entropy [16]byte
			if _, err := rand.Read(entropy[:]); err != nil {
				t.Fatal(err)
			}
			nonce := hex.EncodeToString(entropy[:])
			directory := path.Join(parent, ".dragfm-"+nonce)
			if err := source.MkdirAll(ctx, directory, 0700); err != nil {
				t.Fatal(err)
			}
			created := time.Now().UTC().Add(-48 * time.Hour)
			marker, _ := json.Marshal(map[string]any{"version": 2, "nonce": nonce, "created": created})
			writeRemoteFile(t, ctx, source, path.Join(directory, ".dragfm-owner-v1"), string(marker))
			if err := source.Chmod(ctx, path.Join(directory, ".dragfm-owner-v1"), 0600); err != nil {
				t.Fatal(err)
			}
			writeRemoteFile(t, ctx, source, path.Join(directory, "archive.tar.gz"), "retained archive evidence")
			writeRemoteFile(t, ctx, source, path.Join(parent, "user-file.txt"), "not a workspace")
			if candidate.elevated && !candidate.uploaderOwned {
				if err := source.Exec(ctx, "sudo -n chown -R root:root -- "+shellQuote(directory), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if candidate.uploaderOwned {
				// sudo helpers are uploaded by the ordinary SSH account. Only
				// their descendants (e.g. a Hans identity directory) may be root.
				protected := path.Join(directory, "root-child")
				if err := source.Exec(ctx, "sudo -n mkdir -m 0700 -- "+shellQuote(protected)+" && sudo -n touch -- "+shellQuote(path.Join(protected, "identity")), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			var stats bytes.Buffer
			if err := source.Exec(ctx, "stat -Lc '%d:%i' -- "+shellQuote(parent)+"; sudo -n stat -c '%d:%i:%u:%a' -- "+shellQuote(directory), endpoint.ExecOptions{Stdout: &stats}); err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(stats.String())
			if len(fields) != 2 {
				t.Fatalf("filesystem identity: %q", stats.String())
			}
			dirFields := strings.Split(fields[1], ":")
			if len(dirFields) != 4 {
				t.Fatal("invalid real inode identity")
			}
			uid, err := strconv.ParseUint(dirFields[2], 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := source.Identity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if identity.MachineID == "" || identity.Fingerprint == "" {
				t.Fatal("disposable endpoint has no verified identity")
			}
			digest := sha256.Sum256(marker)
			record := map[string]any{"host_id": document.Hosts[0].ID, "fingerprint": identity.Fingerprint, "machine_id": identity.MachineID, "elevated": candidate.elevated, "path": directory, "parent_id": fields[0], "directory_id": fields[1], "owner_uid": uid, "marker_sha256": hex.EncodeToString(digest[:]), "created_at": created}
			partial := path.Join(parent, "payload.dragfm-partial-"+nonce)
			if candidate.partial != "" {
				switch candidate.partial {
				case "link":
					if err := source.Symlink(ctx, path.Join(parent, "user-file.txt"), partial); err != nil {
						t.Fatal(err)
					}
				case "tree":
					child := path.Join(partial, "readonly")
					if err := source.MkdirAll(ctx, child, 0700); err != nil {
						t.Fatal(err)
					}
					writeRemoteFile(t, ctx, source, path.Join(child, "data"), "owned tree data")
					if err := source.Symlink(ctx, path.Join(parent, "user-file.txt"), path.Join(partial, "outside-link")); err != nil {
						t.Fatal(err)
					}
					if err := source.Chmod(ctx, child, 0000); err != nil {
						t.Fatal(err)
					}
					if err := source.Chmod(ctx, partial, 0500); err != nil {
						t.Fatal(err)
					}
				default:
					writeRemoteFile(t, ctx, source, partial, "owned partial data")
				}
				var fileID bytes.Buffer
				if err := source.Exec(ctx, "stat -c '%d:%i' -- "+shellQuote(partial), endpoint.ExecOptions{Stdout: &fileID}); err != nil {
					t.Fatal(err)
				}
				pinned := map[string]any{"path": partial, "parent_id": fields[0], "file_id": strings.TrimSpace(fileID.String())}
				if candidate.partial == "unpinned" {
					delete(pinned, "file_id")
				}
				record["partials"] = []any{pinned}
				if candidate.partial == "replaced" {
					if err := source.Rename(ctx, partial, partial+"-original", false); err != nil {
						t.Fatal(err)
					}
					writeRemoteFile(t, ctx, source, partial, "replacement user data")
				}
			}
			if candidate.intent {
				delete(record, "directory_id")
			}
			if candidate.badHost {
				record["fingerprint"] = "SHA256:unrelated-fixture-host"
			}
			if candidate.badMarker {
				writeRemoteFile(t, ctx, source, path.Join(directory, ".dragfm-owner-v1"), string(marker)+" ")
				if err := source.Chmod(ctx, path.Join(directory, ".dragfm-owner-v1"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if candidate.replaceParent {
				if err := source.Rename(ctx, parent, moved, false); err != nil {
					t.Fatal(err)
				}
				if err := source.Exec(ctx, "cp -a -- "+shellQuote(moved)+" "+shellQuote(parent), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			app := unlockedTestApp(t)
			encoded, _ := json.Marshal(document)
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &raw); err != nil {
				t.Fatal(err)
			}
			raw["workspaces"], _ = json.Marshal([]any{record})
			encoded, _ = json.Marshal(raw)
			app.mu.Lock()
			err = json.Unmarshal(encoded, &app.document)
			app.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(app.vaultPath); err != nil || bytes.Contains(data, []byte(directory)) {
				t.Fatalf("recovery metadata is not confined to encrypted vault: %v", err)
			}
			if err := app.Lock(); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Unlock("test master password"); err != nil {
				t.Fatal(err)
			}
			terminal := make(chan JobUpdateModel, 4)
			var challenges atomic.Int32
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				switch name {
				case "job:update":
					update := value.(JobUpdateModel)
					if strings.HasPrefix(update.Description, "清理过期临时资源 · ") && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
						terminal <- update
					}
				case "challenge":
					challenge := value.(ChallengeModel)
					challenges.Add(1)
					if candidate.skip {
						if skipper, ok := any(app).(interface{ SkipChallenge(string) }); ok {
							skipper.SkipChallenge(challenge.ID)
						} else {
							app.ResolveChallenge(challenge.ID, false, "", false)
						}
					} else {
						app.ResolveChallenge(challenge.ID, true, "", false)
					}
				}
			}
			app.mu.Unlock()
			listing, err := app.ChangeEndpoint(LeftPane, "ci-source", "")
			if err != nil {
				t.Fatal(err)
			}
			if listing.Endpoint != "ci-source" {
				t.Fatal("recovery replaced browsing endpoint")
			}
			var final JobUpdateModel
			completion := time.NewTimer(20 * time.Second)
			defer completion.Stop()
			select {
			case final = <-terminal:
			case <-completion.C:
				t.Fatal("reconnection never produced a terminal recovery job within its fixture deadline")
			case <-ctx.Done():
				t.Fatal("reconnection never produced a terminal recovery job", ctx.Err())
			}
			unsafePartial := candidate.partial == "replaced" || candidate.partial == "unpinned"
			preserve := candidate.skip || candidate.badHost || candidate.badMarker || candidate.replaceParent || unsafePartial
			wantState := "succeeded"
			if candidate.badHost || candidate.badMarker || candidate.replaceParent || unsafePartial {
				wantState = "failed"
			}
			if final.State != wantState {
				t.Fatalf("recovery ended %s: %s", final.State, final.Message)
			}
			if candidate.elevated && challenges.Load() != 1 {
				t.Fatalf("root cleanup required exactly one permission decision, got %d", challenges.Load())
			}
			if !candidate.elevated && challenges.Load() != 0 {
				t.Fatal("ordinary recovery requested elevated permission")
			}
			if preserve {
				var contents bytes.Buffer
				if err := source.Exec(ctx, "sudo -n cat -- "+shellQuote(path.Join(directory, "archive.tar.gz")), endpoint.ExecOptions{Stdout: &contents}); err != nil || contents.String() != "retained archive evidence" {
					t.Fatalf("unsafe recovery changed retained data: %v", err)
				}
			} else if _, err := source.Stat(ctx, directory); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expired owned workspace was not removed: %v", err)
			}
			assertRemoteFile(t, ctx, source, path.Join(parent, "user-file.txt"), "not a workspace")
			if candidate.partial != "" {
				switch candidate.partial {
				case "replaced":
					assertRemoteFile(t, ctx, source, partial, "replacement user data")
					assertRemoteFile(t, ctx, source, partial+"-original", "owned partial data")
				case "unpinned":
					assertRemoteFile(t, ctx, source, partial, "owned partial data")
				default:
					if _, err := source.Stat(ctx, partial); !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("pinned partial was not recovered: %v", err)
					}
				}
			}
			if _, err := app.List(LeftPane, "ci-source", parent); err != nil {
				t.Fatalf("recovery disrupted browsing: %v", err)
			}
			if err := app.Lock(); err != nil {
				t.Fatal(err)
			}
			_, saved, err := vault.Open(app.vaultPath, []byte("test master password"))
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ = json.Marshal(saved)
			var decoded struct {
				Workspaces []json.RawMessage `json:"workspaces"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			wantRecords := 0
			if preserve {
				wantRecords = 1
			}
			if len(decoded.Workspaces) != wantRecords {
				t.Fatalf("journal records=%d want=%d", len(decoded.Workspaces), wantRecords)
			}
			if len(saved.History) != 1 || !strings.Contains(saved.History[0].Operation, "清理过期临时资源") {
				t.Fatalf("recovery did not persist a visible history result: %s", fmt.Sprint(saved.History))
			}
			if _, err := app.Unlock("test master password"); err != nil {
				t.Fatal(err)
			}
			if jobs, err := app.JobSnapshot(); err != nil || len(jobs) != 0 {
				t.Fatalf("unlock restored old pending work: %v", err)
			}
		})
	}
}
