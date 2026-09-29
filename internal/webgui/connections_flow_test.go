//go:build !windows

package webgui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
)

// Exercise the actual Wails-bound methods using their JSON contract. This
// keeps the acceptance flow compilable on the baseline, where missing controls
// or unsupported Markdown produce a behavioral failure, not a compile error.
func connectionRPC(app *App, name string, args ...any) (json.RawMessage, error) {
	method := reflect.ValueOf(app).MethodByName(name)
	if !method.IsValid() {
		return nil, fmt.Errorf("configuration action %s is unavailable", name)
	}
	if method.Type().NumIn() != len(args) {
		return nil, errors.New("configuration action has an incompatible input contract")
	}
	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		data, err := json.Marshal(arg)
		if err != nil {
			return nil, err
		}
		value := reflect.New(method.Type().In(i))
		if err := json.Unmarshal(data, value.Interface()); err != nil {
			return nil, err
		}
		values[i] = value.Elem()
	}
	result := method.Call(values)
	if !result[len(result)-1].IsNil() {
		return nil, result[len(result)-1].Interface().(error)
	}
	return json.Marshal(result[0].Interface())
}

func configurationRevision(t *testing.T, app *App) string {
	t.Helper()
	value, err := app.GetConfigTexts()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	var wire struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Revision == "" {
		t.Fatal("configuration cannot protect an open draft against concurrent credential saves")
	}
	return wire.Revision
}

func TestConnectionManagementSelectedLoginPolicyAndStaleDraft(t *testing.T) {
	app := unlockedTestApp(t)
	address, pin := passwordRouteFixture(t, "fixture-login-password", nil)
	guard, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var unwanted atomic.Int64
	guardDone := make(chan struct{})
	go func() {
		defer close(guardDone)
		for {
			conn, err := guard.Accept()
			if err != nil {
				return
			}
			unwanted.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = guard.Close(); <-guardDone })
	markdown := fmt.Sprintf("#主机\n##selected\ntester:fixture-login-password@%s\n##quiet\ntester:fixture-never-send@%s\n###允许跳板\nfalse\n##other\ntester:fixture-never-send@%s\n#私钥\n#socks池\n", address, guard.Addr(), guard.Addr())
	if _, err := app.SaveConfigTexts(markdown); err != nil {
		t.Fatal("endpoint-only policy is not accepted by the real config editor: ", err)
	}
	app.mu.Lock()
	selected, _ := app.document.HostByName("selected")
	quiet, _ := app.document.HostByName("quiet")
	other, _ := app.document.HostByName("other")
	app.document.HostByID(selected.ID).HopFingerprints = []string{pin}
	app.sessionSSH[other.ID], app.sessionSSH[quiet.ID] = true, true
	app.mu.Unlock()
	events := observeCommands(t, app)
	call := func(name string, args ...any) json.RawMessage {
		t.Helper()
		value, err := connectionRPC(app, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	overview := call("GetConnectionOverview")
	if strings.Contains(string(overview), "fixture-login-password") || strings.Contains(string(overview), "fixture-never-send") {
		t.Fatal("status view exposed a credential instead of saved metadata")
	}
	request := map[string]any{"kind": "ssh", "id": selected.ID, "revision": configurationRevision(t, app)}
	var jobID string
	if err := json.Unmarshal(call("QueueConnectionTest", request), &jobID); err != nil {
		t.Fatal(err)
	}
	result := awaitCommandEvent(t, events, jobID, func(job outputJobWire) bool { return job.State == "succeeded" || job.State == "failed" })
	if result.State != "succeeded" || !strings.Contains(result.Message, "不是 TCP RTT") {
		t.Fatal("selected real SSH login was not reported with its correct measurement scope")
	}
	if _, err := app.ChangeEndpoint(LeftPane, selected.Name, ""); err != nil {
		t.Fatal(err)
	}
	before, err := app.pane(LeftPane)
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	left, right := orderedPair(quiet.ID, other.ID)
	app.document.Relays = append(app.document.Relays, config.RelaySuccess{EndpointAID: left, EndpointBID: right, RelayHostID: selected.ID, LastSuccess: time.Now()})
	app.mu.Unlock()
	cacheRevision := configurationRevision(t, app)
	call("ClearRelayCache", quiet.ID, other.ID, cacheRevision)
	app.mu.RLock()
	remaining := len(app.document.Relays)
	app.mu.RUnlock()
	if remaining != 0 {
		t.Fatal("forgetting a route left it available for the next automatic attempt")
	}
	if _, err := connectionRPC(app, "SaveConfigTextsAtRevision", markdown, cacheRevision); err == nil {
		t.Fatal("cache removal did not invalidate an already-open configuration revision")
	}
	app.mu.Lock()
	app.document.Relays = append(app.document.Relays, config.RelaySuccess{EndpointAID: left, EndpointBID: right, RelayHostID: selected.ID, LastSuccess: time.Now()})
	app.mu.Unlock()
	oldRevision := configurationRevision(t, app)
	call("SetConnectionPolicy", "ssh", selected.ID, false, false, oldRevision)
	after, err := app.pane(LeftPane)
	if err != nil || after.endpoint != before.endpoint {
		t.Fatal("excluding a relay replaced or invalidated the active browsing endpoint")
	}
	if _, err := after.endpoint.Home(context.Background()); err != nil {
		t.Fatal("excluding a relay closed its active SSH transport")
	}
	app.mu.RLock()
	remaining = len(app.document.Relays)
	app.mu.RUnlock()
	if remaining != 0 {
		t.Fatal("relay opt-out left its pair cache eligible")
	}
	candidates, err := app.routeCandidates(quiet, other.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.relayID == selected.ID {
			t.Fatal("endpoint-only host remained an automatic relay")
		}
	}
	if _, err := connectionRPC(app, "SaveConfigTextsAtRevision", markdown, oldRevision); err == nil {
		t.Fatal("stale editor silently overwrote a newer connection policy")
	}
	// A failed explicit test must not fall through to another eligible saved
	// session. Use a closed loopback port; the unrelated guard is still live.
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	missing := closed.Addr().String()
	_ = closed.Close()
	current, err := app.GetConfigTexts()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveConfigTexts(strings.ReplaceAll(current.Markdown, address, missing)); err != nil {
		t.Fatal(err)
	}
	request["revision"] = configurationRevision(t, app)
	if err := json.Unmarshal(call("QueueConnectionTest", request), &jobID); err != nil {
		t.Fatal(err)
	}
	result = awaitCommandEvent(t, events, jobID, func(job outputJobWire) bool { return job.State == "succeeded" || job.State == "failed" })
	if result.State != "failed" {
		t.Fatal("test of the unavailable selected route was reported as success")
	}
	_ = guard.Close()
	<-guardDone
	if unwanted.Load() != 0 {
		t.Fatal("status viewing or explicit selected-route test dialed an unrelated SSH session")
	}
}
