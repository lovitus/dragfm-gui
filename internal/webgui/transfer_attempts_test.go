package webgui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

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
	median, ok := probeTCPMedian(prober, "proxy.example:1080")
	if !ok || median != 40*time.Millisecond || prober.calls != 3 {
		t.Fatalf("median=%s ok=%t calls=%d", median, ok, prober.calls)
	}

	prober = &scriptedTCPProber{results: []struct {
		duration time.Duration
		err      error
	}{
		{duration: 10 * time.Millisecond},
		{err: errors.New("timeout")},
		{err: errors.New("timeout")},
	}}
	if _, ok := probeTCPMedian(prober, "proxy.example:1080"); ok || prober.calls != 3 {
		t.Fatalf("one successful sample must not pass: ok=%t calls=%d", ok, prober.calls)
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
		{strategy.TargetPull, true, strategy.Rsync, strategy.TargetSudoRisk},
		{strategy.TargetPull, true, strategy.SCP, strategy.TargetSudoRisk},
		{strategy.TargetPull, true, strategy.EncryptedStream, strategy.TargetSudoRisk},
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

func TestNcatTarConsumerGroupsEveryReceiveStepInPipeline(t *testing.T) {
	consumer := ncatTarConsumer("/tmp/stage", "source file", "/tmp/partial")
	if !strings.HasPrefix(consumer, "{ ") || !strings.HasSuffix(consumer, "; }") {
		t.Fatalf("consumer is not a grouped pipeline command: %s", consumer)
	}
	if strings.Contains(consumer, "| mkdir") {
		t.Fatalf("consumer would discard the archive in mkdir: %s", consumer)
	}
}

func TestConfiguredRootRouteChangesOnlyFinalHopIdentity(t *testing.T) {
	route := connector.Route{Hops: []connector.Hop{
		{Host: "relay", User: "jump", Credentials: connector.Credentials{Password: "jump-pass"}},
		{Host: "target", User: "regular", Credentials: connector.Credentials{Password: "regular-pass", UseAgent: true}},
	}}
	root, err := configuredRootRoute(route, "root-user", "root-pass")
	if err != nil {
		t.Fatal(err)
	}
	if root.Hops[0].User != "jump" || root.Hops[0].Credentials.Password != "jump-pass" {
		t.Fatalf("jump identity changed: %#v", root.Hops[0])
	}
	if root.Hops[1].User != "root-user" || root.Hops[1].Credentials.Password != "root-pass" || !root.Hops[1].Credentials.UseAgent {
		t.Fatalf("root final hop=%#v", root.Hops[1])
	}
	if route.Hops[1].User != "regular" || route.Hops[1].Credentials.Password != "regular-pass" {
		t.Fatalf("input route was mutated: %#v", route.Hops[1])
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
