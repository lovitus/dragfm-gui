package webgui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// These are the real policy plan and state machine; only the terminal action
// records whether authorization allowed dispatch, with no network substitute.
func TestNativeRootPairRequiresBothEndpointApprovals(t *testing.T) {
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP} {
			for _, denied := range []strategy.Risk{"", strategy.SourceSudoRisk, strategy.TargetSudoRisk} {
				t.Run(fmt.Sprintf("%s/%s/deny=%s", direction, method, denied), func(t *testing.T) {
					plan := directRemotePlan(transfer.PreflightReport{}, true)
					runs := 0
					for i := range plan {
						if plan[i].Elevated && plan[i].Direction == direction && plan[i].Method == method {
							plan[i].Run = func(context.Context) error { runs++; return nil }
						}
					}
					asked := make(map[strategy.Risk]bool)
					err := strategy.Execute(context.Background(), plan, func(_ context.Context, risk strategy.Risk, _ strategy.Attempt) error {
						asked[risk] = true
						if risk == denied {
							return strategy.ErrRiskSkipped
						}
						return nil
					}, nil)
					if denied != "" {
						if err == nil || runs != 0 || !asked[denied] {
							t.Fatal("denied endpoint still allowed root-pair transfer dispatch")
						}
					} else if err != nil || runs != 1 || !asked[strategy.SourceSudoRisk] || !asked[strategy.TargetSudoRisk] {
						t.Fatal("root-pair dispatch did not require two independent approvals")
					}
				})
			}
		}
	}
}

// Baseline-compatible behavior regression: only an elevated stream is
// available. Permission to run sudo must not authorize its network listener.
func TestElevatedTransferRequiresIndependentListenerApproval(t *testing.T) {
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		for _, method := range []strategy.Method{strategy.EncryptedStream, strategy.NcatTar} {
			for _, deny := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/deny=%t", direction, method, deny), func(t *testing.T) {
					preflight := transfer.PreflightReport{SourceCapabilities: transfer.EndpointCapabilities{Tools: map[string]bool{"ncat": true}}, TargetCapabilities: transfer.EndpointCapabilities{Tools: map[string]bool{"ncat": true}}}
					plan := directRemotePlan(preflight, true)
					runs := 0
					for i := range plan {
						if plan[i].Elevated && plan[i].Direction == direction && plan[i].Method == method {
							plan[i].Run = func(context.Context) error { runs++; return nil }
						}
					}
					decisions := make(map[strategy.Risk]int)
					err := strategy.Execute(context.Background(), plan, func(_ context.Context, risk strategy.Risk, _ strategy.Attempt) error {
						decisions[risk]++
						if risk == strategy.ListenRisk && deny {
							return errors.New("fixture listener denied")
						}
						return nil
					}, nil)
					privilege := strategy.SourceSudoRisk
					if direction == strategy.TargetPull {
						privilege = strategy.TargetSudoRisk
					}
					if decisions[privilege] != 1 || decisions[strategy.ListenRisk] != 1 {
						t.Fatal("sudo approval hid the independent listener decision")
					}
					if deny {
						if err == nil || runs != 0 {
							t.Fatal("listener denial still started the transfer")
						}
					} else if err != nil || runs != 1 {
						t.Fatalf("approved transfer did not run once: %v", err)
					}
				})
			}
		}
	}
}

func TestLocalDestinationWritePreflight(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	writable := filepath.Join(root, "writable")
	protected := filepath.Join(root, "protected")
	if err := os.Mkdir(writable, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(protected, 0700); err != nil {
		t.Fatal(err)
	}

	local := endpoint.NewLocal()
	operation := transfer.Operation{Source: local, Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: filepath.Join(writable, "target.txt")}
	if elevated, _ := localDestinationRequiresSudo(context.Background(), operation); elevated {
		t.Fatal("writable destination was classified as requiring sudo")
	}
	operation.TargetPath = filepath.Join(protected, "target.txt")
	permissionProbe := func(directory string) error {
		if directory == protected {
			return os.ErrPermission
		}
		return probeDirectoryWritable(directory)
	}
	if elevated, directory := localDestinationRequiresSudoWithProbe(context.Background(), operation, permissionProbe); !elevated || directory != protected {
		t.Fatalf("protected destination not detected: elevated=%v directory=%q", elevated, directory)
	}
	app := New(filepath.Join(root, "unused.vault"))
	attempts, approval := app.transferAttemptsWithSudoProbe(operation, transfer.PreflightReport{}, permissionProbe)
	if len(attempts) != 1 || !attempts[0].Elevated || attempts[0].Risk != strategy.SudoRisk || approval == nil {
		t.Fatalf("unexpected protected-directory strategy: %#v approval=%v", attempts, approval != nil)
	}
}

type scriptedTCPProber struct {
	results []struct {
		duration time.Duration
		err      error
	}
	calls int
}

func (p *scriptedTCPProber) ProbeTCP(string) (time.Duration, error) {
	result := p.results[p.calls]
	p.calls++
	return result.duration, result.err
}

func TestProbeTCPMedianRequiresTwoOfThreeSamples(t *testing.T) {
	prober := &scriptedTCPProber{results: []struct {
		duration time.Duration
		err      error
	}{
		{duration: 90 * time.Millisecond},
		{duration: 10 * time.Millisecond},
		{duration: 40 * time.Millisecond},
	}}
	median, err := probeTCPMedian(prober, "proxy.example:1080")
	if err != nil || median != 40*time.Millisecond || prober.calls != 3 {
		t.Fatalf("median=%s err=%v calls=%d", median, err, prober.calls)
	}

	prober = &scriptedTCPProber{results: []struct {
		duration time.Duration
		err      error
	}{
		{duration: 10 * time.Millisecond},
		{err: errors.New("timeout")},
		{err: errors.New("timeout")},
	}}
	if _, err := probeTCPMedian(prober, "proxy.example:1080"); err == nil || prober.calls != 3 {
		t.Fatalf("one successful sample must not pass: err=%v calls=%d", err, prober.calls)
	}
}

func TestLocalRemoteAttemptOrderStartsWithRsyncThenSCP(t *testing.T) {
	preflight := transfer.PreflightReport{
		SourceCapabilities: transfer.EndpointCapabilities{Tools: map[string]bool{"ncat": true}},
		TargetCapabilities: transfer.EndpointCapabilities{Tools: map[string]bool{"ncat": true}},
	}
	plan := directRemotePlan(preflight, true)
	type want struct {
		direction strategy.Direction
		elevated  bool
		method    strategy.Method
		risk      strategy.Risk
	}
	wanted := []want{
		{strategy.SourcePush, false, strategy.Rsync, ""},
		{strategy.SourcePush, false, strategy.SCP, ""},
		{strategy.SourcePush, false, strategy.EncryptedStream, strategy.ListenRisk},
		{strategy.SourcePush, false, strategy.NcatTar, strategy.ListenRisk},
		{strategy.TargetPull, false, strategy.Rsync, ""},
		{strategy.TargetPull, false, strategy.SCP, ""},
		{strategy.TargetPull, false, strategy.EncryptedStream, strategy.ListenRisk},
		{strategy.TargetPull, false, strategy.NcatTar, strategy.ListenRisk},
		{strategy.SourcePush, true, strategy.Rsync, strategy.SourceSudoRisk},
		{strategy.SourcePush, true, strategy.SCP, strategy.SourceSudoRisk},
		{strategy.SourcePush, true, strategy.EncryptedStream, strategy.SourceSudoRisk},
		{strategy.SourcePush, true, strategy.NcatTar, strategy.SourceSudoRisk},
		{strategy.TargetPull, true, strategy.Rsync, strategy.TargetSudoRisk},
		{strategy.TargetPull, true, strategy.SCP, strategy.TargetSudoRisk},
		{strategy.TargetPull, true, strategy.EncryptedStream, strategy.TargetSudoRisk},
		{strategy.TargetPull, true, strategy.NcatTar, strategy.TargetSudoRisk},
	}
	if len(plan) != len(wanted) {
		t.Fatalf("plan length=%d want=%d: %#v", len(plan), len(wanted), plan)
	}
	for index, expected := range wanted {
		got := plan[index]
		if got.Direction != expected.direction || got.Elevated != expected.elevated || got.Method != expected.method || got.Risk != expected.risk {
			t.Fatalf("plan[%d]=%#v want=%#v", index, got, expected)
		}
	}
}

func TestConfiguredRootRouteChangesOnlyFinalHopIdentity(t *testing.T) {
	for _, candidate := range []struct {
		name, user, password string
		wantAgent, wantError bool
		keys                 []connector.PrivateKey
	}{
		{name: "different-account", user: "root-user", password: "root-pass"},
		{name: "same-account", user: "regular", wantAgent: true},
		{name: "same-account-new-password", user: "regular", password: "new-pass", wantAgent: true},
		{name: "missing-root-credentials", user: "root-user", wantError: true},
		{name: "different-account-key-only", user: "root-user", keys: []connector.PrivateKey{{PEM: []byte("explicit-root-key"), Passphrase: []byte("root-key-phrase")}}},
		{name: "different-account-key-and-password", user: "root-user", password: "root-pass", keys: []connector.PrivateKey{{PEM: []byte("explicit-root-key")}}},
		{name: "same-account-explicit-key", user: "regular", keys: []connector.PrivateKey{{PEM: []byte("explicit-root-key")}}},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			route := connector.Route{Hops: []connector.Hop{
				{Host: "relay", User: "jump", Credentials: connector.Credentials{Password: "jump-pass"}},
				{Host: "target", User: "regular", Credentials: connector.Credentials{Password: "regular-pass", UseAgent: true, PrivateKeys: []connector.PrivateKey{{PEM: []byte("account-bound-key")}}}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "host-pin"}},
			}}
			root, err := configuredRootRoute(route, candidate.user, candidate.password, candidate.keys...)
			if candidate.wantError {
				if err == nil {
					t.Fatal("attempted a different account without its credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if root.Hops[0].User != "jump" || root.Hops[0].Credentials.Password != "jump-pass" {
				t.Fatalf("jump identity changed: %#v", root.Hops[0])
			}
			wantPassword := candidate.password
			if candidate.user == "regular" && wantPassword == "" {
				wantPassword = "regular-pass"
			}
			if root.Hops[1].User != candidate.user || root.Hops[1].Credentials.Password != wantPassword || root.Hops[1].Credentials.UseAgent != candidate.wantAgent {
				t.Fatalf("root final hop=%#v", root.Hops[1])
			}
			if (len(root.Hops[1].Credentials.PrivateKeys) != 0) != (candidate.wantAgent || len(candidate.keys) > 0) || root.Hops[1].HostKey.PinnedSHA256 != "host-pin" {
				t.Fatal("private key crossed account boundary or host pin was lost")
			}
			if len(candidate.keys) > 0 {
				key := root.Hops[1].Credentials.PrivateKeys[0]
				if string(key.PEM) != string(candidate.keys[0].PEM) || string(key.Passphrase) != string(candidate.keys[0].Passphrase) || len(root.Hops[1].Credentials.PrivateKeys) != len(candidate.keys) {
					t.Fatal("explicit root keys were replaced, appended to ordinary credentials or lost their passphrase")
				}
			}
			if route.Hops[1].User != "regular" || route.Hops[1].Credentials.Password != "regular-pass" {
				t.Fatalf("input route was mutated: %#v", route.Hops[1])
			}
		})
	}
}

func TestSameMachineAttemptPrecedesNetworkMethods(t *testing.T) {
	root := t.TempDir()
	app := New(filepath.Join(root, "unused.vault"))
	local := endpoint.NewLocal()
	operation := transfer.Operation{
		Source:      local,
		Destination: endpoint.NewLocal(),
		SourcePath:  filepath.Join(root, "source"),
		TargetPath:  filepath.Join(root, "target"),
	}
	attempts, _ := app.transferAttemptsWithSudoProbe(operation, transfer.PreflightReport{SameMachine: true}, func(string) error { return nil })
	if len(attempts) == 0 {
		t.Fatal("same-machine plan is empty")
	}
	first := attempts[0]
	if first.Tier != strategy.SameHost || first.Method != strategy.MemoryStream || first.Run == nil {
		t.Fatalf("first same-machine attempt = %#v", first)
	}
}
