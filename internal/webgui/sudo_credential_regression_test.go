package webgui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

// Approval is not authentication. A job can stop after the prompt, or use a
// configured root route without ever testing the offered sudo credential.
func TestSudoApprovalDoesNotPersistUnverifiedPasswordOrLeakItFromJob(t *testing.T) {
	address, pin := passwordRouteFixture(t, "connection-password", nil)
	const offered = "unverified-sudo-fixture"
	const saved = offered + "-previous-longer-secret"
	app := unlockedTestApp(t)
	host := config.Host{ID: "fixture", Name: "fixture", RouteSpec: fmt.Sprintf("tester:connection-password@%s", address), HopFingerprints: []string{pin}, SudoPassword: saved}
	app.mu.Lock()
	app.document.Hosts = []config.Host{host}
	app.mu.Unlock()
	if err := app.save(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	remote, _, err := app.connectRemote(ctx, host.Name, "本机")
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	done := make(chan JobUpdateModel, 1)
	app.mu.Lock()
	generation := app.generation
	app.eventSink = func(name string, value any) {
		if name == "challenge" {
			challenge := value.(ChallengeModel)
			app.ResolveChallenge(challenge.ID, challenge.Secret, offered, true)
		}
		if name == "job:update" {
			update := value.(JobUpdateModel)
			if update.ID == "sudo-offer" && update.State == "failed" {
				select {
				case done <- update:
				default:
				}
			}
		}
	}
	app.mu.Unlock()
	_, err = app.submitFor(generation, jobs.Job{ID: "sudo-offer", Run: func(ctx context.Context, emit func(jobs.Update)) error {
		operation := transfer.Operation{Source: remote, Destination: endpoint.NewLocal()}
		approve := app.transferApproval("", nil, operation, make(map[strategy.Direction]string))
		if err := approve(ctx, strategy.SourceSudoRisk, strategy.Attempt{Direction: strategy.SourcePush, Elevated: true}); err != nil {
			return err
		}
		emit(jobs.Update{Message: "fixture diagnostic " + offered + " / " + saved})
		return errors.New("fixture stops before authenticating sudo: " + offered + " / " + saved)
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	app.mu.RLock()
	inMemory := app.document.Hosts[0].SudoPassword
	app.mu.RUnlock()
	_, document, err := vault.Open(app.vaultPath, []byte("test master password"))
	if err != nil {
		t.Fatal(err)
	}
	if inMemory != saved || document.Hosts[0].SudoPassword != saved {
		t.Fatal("offering an unverified sudo password replaced the working saved credential")
	}
	for _, update := range app.queue.Snapshot() {
		text := update.Message + update.Error
		if update.ID == "sudo-offer" && (strings.Contains(text, offered) || strings.Contains(text, "previous-longer-secret")) {
			t.Fatal("a newly offered or overlapping saved credential leaked into authoritative queue state")
		}
	}
}
