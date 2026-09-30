//go:build !windows

package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type identifiedEndpoint struct {
	endpoint.Endpoint
	identity endpoint.Identity
}

func (e identifiedEndpoint) Identity(context.Context) (endpoint.Identity, error) {
	return e.identity, nil
}

func TestPreflightRequiresConsistentHostIdentity(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(from, []byte("identity-bound move"), 0600); err != nil {
		t.Fatal(err)
	}
	sshID := endpoint.Identity{Kind: endpoint.SSHKind, MachineID: "cloned-id", Fingerprint: "SHA256:fixture-key-a", Principal: "tester", RootView: "1:2"}
	aliasID := sshID
	aliasID.Name = "alias"
	otherUser := sshID
	otherUser.Principal = "other"
	otherRoot := sshID
	otherRoot.RootView = "1:3"
	unknownRoot := sshID
	unknownRoot.RootView = ""
	localID := endpoint.Identity{Kind: endpoint.LocalKind, MachineID: "cloned-id"}
	for _, test := range []struct {
		name        string
		left, right endpoint.Identity
		same        bool
	}{
		{name: "local panes", left: localID, right: localID, same: true},
		{name: "verified SSH aliases", left: sshID, right: aliasID, same: true},
		{name: "different login accounts", left: sshID, right: otherUser},
		{name: "different root views", left: sshID, right: otherRoot},
		{name: "unverified root view", left: sshID, right: unknownRoot},
		{name: "conflicting SSH keys", left: sshID, right: endpoint.Identity{Kind: endpoint.SSHKind, MachineID: sshID.MachineID, Fingerprint: "SHA256:fixture-key-b"}},
		{name: "missing SSH key", left: sshID, right: endpoint.Identity{Kind: endpoint.SSHKind, MachineID: sshID.MachineID}},
		{name: "mixed local and SSH", left: localID, right: sshID},
		{name: "untyped identity", left: endpoint.Identity{MachineID: "cloned-id"}, right: endpoint.Identity{MachineID: "cloned-id"}},
		{name: "missing machine ID", left: endpoint.Identity{Kind: endpoint.SSHKind, Fingerprint: sshID.Fingerprint}, right: endpoint.Identity{Kind: endpoint.SSHKind, Fingerprint: sshID.Fingerprint}},
		{name: "conflicting machine IDs", left: sshID, right: endpoint.Identity{Kind: endpoint.SSHKind, MachineID: "other-id", Fingerprint: sshID.Fingerprint}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Preflight(context.Background(), Operation{
				Source: identifiedEndpoint{endpoint.NewLocal(), test.left}, Destination: identifiedEndpoint{endpoint.NewLocal(), test.right},
				SourcePath: from, TargetPath: to,
			})
			if err != nil {
				t.Fatal(err)
			}
			if report.SameMachine != test.same {
				t.Fatalf("same-machine classification=%t, want %t", report.SameMachine, test.same)
			}
		})
	}
}
