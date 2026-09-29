//go:build !windows

package webgui

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
)

type commandFixtureStep struct {
	Stdout []byte
	Stderr []byte
	Exit   *int
}

// A real child process under Local.Exec, with an event-driven test gate instead
// of sleep/poll loops. It cannot finish until the parent has seen live output.
func TestQueuedCommandFixtureProcess(t *testing.T) {
	address := os.Getenv("DRAGFM_COMMAND_OUTPUT_FIXTURE")
	if address == "" {
		return
	}
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		os.Exit(90)
	}
	defer connection.Close()
	decoder, encoder := json.NewDecoder(connection), json.NewEncoder(connection)
	for {
		var step commandFixtureStep
		if decoder.Decode(&step) != nil {
			os.Exit(91)
		}
		if step.Exit != nil {
			os.Exit(*step.Exit)
		}
		if _, err := os.Stdout.Write(step.Stdout); err != nil {
			os.Exit(92)
		}
		if _, err := os.Stderr.Write(step.Stderr); err != nil {
			os.Exit(93)
		}
		if encoder.Encode(true) != nil {
			os.Exit(94)
		}
	}
}

// These wire projections intentionally use the baseline's existing API. When
// overlaid on the old tree, this must fail for absent live output, not because
// a newly introduced Go field or helper symbol does not compile.
type outputJobWire struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	Description string `json:"description"`
	Message     string `json:"message"`
	Output      string `json:"output"`
}

func commandWire(value any) outputJobWire {
	data, _ := json.Marshal(value)
	var wire outputJobWire
	_ = json.Unmarshal(data, &wire)
	return wire
}

func fixtureCommand(t *testing.T, app *App) (string, func(commandFixtureStep)) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("DRAGFM_COMMAND_OUTPUT_FIXTURE=%s %s -test.run=^TestQueuedCommandFixtureProcess$", shellQuote(listener.Addr().String()), shellQuote(executable))
	id, err := app.QueueCommand("控制机", command)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal("queued child did not connect to its isolated fixture gate: ", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	decoder, encoder := json.NewDecoder(connection), json.NewEncoder(connection)
	return id, func(step commandFixtureStep) {
		t.Helper()
		_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
		if err := encoder.Encode(step); err != nil {
			t.Fatal(err)
		}
		if step.Exit != nil {
			return
		}
		var ok bool
		if err := decoder.Decode(&ok); err != nil || !ok {
			t.Fatal("fixture child did not acknowledge its actual write")
		}
	}
}

func observeCommands(t *testing.T, app *App) <-chan outputJobWire {
	t.Helper()
	events := make(chan outputJobWire, 1024)
	app.mu.Lock()
	app.eventSink = func(name string, value any) {
		if name == "job:update" {
			events <- commandWire(value)
		}
	}
	app.mu.Unlock()
	return events
}

func awaitCommandEvent(t *testing.T, events <-chan outputJobWire, id string, match func(outputJobWire) bool) outputJobWire {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.ID == id && match(event) {
				return event
			}
		case <-deadline.C:
			t.Fatal("no matching live command event before the bounded deadline")
		}
	}
}

func TestQueuedCommandLiveOutputPendingFailureCancelAndRestart(t *testing.T) {
	app := unlockedTestApp(t)
	events := observeCommands(t, app)
	id, write := fixtureCommand(t, app)
	pending, err := app.QueueCommand("控制机", "printf 'second-command-complete\\n'")
	if err != nil {
		t.Fatal(err)
	}
	write(commandFixtureStep{Stdout: []byte("first-command-live\n")})
	awaitCommandEvent(t, events, id, func(job outputJobWire) bool {
		return job.State == "running" && strings.Contains(job.Output+job.Message, "first-command-live")
	})
	snapshot, err := app.JobSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	sawPending, sawOutput := false, false
	for _, job := range snapshot {
		if job.ID == pending {
			sawPending = job.State == "pending" && strings.Contains(job.Description, "second-command-complete")
		}
		if job.ID == id {
			sawOutput = strings.Contains(commandWire(job).Output, "first-command-live")
		}
	}
	if !sawPending || !sawOutput {
		t.Fatal("snapshot did not retain live output and the second command's pending preview")
	}
	write(commandFixtureStep{Stderr: []byte("error-tail-without-newline")})
	exit := 19
	write(commandFixtureStep{Exit: &exit})
	failed := awaitCommandEvent(t, events, id, func(job outputJobWire) bool { return job.State == "failed" })
	if !strings.Contains(failed.Output, "first-command-live") || !strings.Contains(failed.Output, "error-tail-without-newline") || !strings.Contains(failed.Message, "19") {
		t.Fatal("failed command lost its output tail or original exit status")
	}
	awaitCommandEvent(t, events, pending, func(job outputJobWire) bool {
		return job.State == "succeeded" && strings.Contains(job.Output, "second-command-complete")
	})
	cancelID, cancelWrite := fixtureCommand(t, app)
	cancelWrite(commandFixtureStep{Stdout: []byte("before-cancel\n")})
	awaitCommandEvent(t, events, cancelID, func(job outputJobWire) bool {
		return job.State == "running" && strings.Contains(job.Output, "before-cancel")
	})
	app.CancelJob(cancelID)
	cancelled := awaitCommandEvent(t, events, cancelID, func(job outputJobWire) bool { return job.State == "cancelled" })
	if !strings.Contains(cancelled.Output, "before-cancel") {
		t.Fatal("cancellation overwrote output")
	}
	if err := app.Lock(); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := app.Unlock("test master password")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []outputJobWire{failed, cancelled} {
		found := false
		for _, item := range bootstrap.History {
			wire := commandWire(item)
			if item.ID == expected.ID {
				found = wire.State == expected.State && wire.Output == expected.Output
			}
		}
		if !found {
			t.Fatal("restart history lost the command state or sanitized transcript")
		}
	}
	remaining, err := app.JobSnapshot()
	if err != nil || len(remaining) != 0 {
		t.Fatal("restart restored in-memory jobs")
	}
}

func TestQueuedCommandStreamingCredentialBoundaries(t *testing.T) {
	cases := []struct {
		name      string
		secret    string
		chunks    []string
		forbidden []string
	}{
		{"known-multiline", "fixture-first-half\nfixture-second-half", []string{"fixture-first-half\n", "fixture-second", "-half\n"}, []string{"fixture-first", "fixture-second"}},
		{"quoted-route", "", []string{"u:\"route-first\n", "route-last\"@192.0.2.8:22\n"}, []string{"route-first", "route-last"}},
		{"quoted-assignment", "", []string{"password=\"assignment-first\n", "assignment-last\"\n"}, []string{"assignment-first", "assignment-last"}},
		{"assignment-on-next-line", "", []string{"password=\n", "fixture-next-line-password\n"}, []string{"fixture-next-line-password"}},
		{"key-marker-inside-quote", "", []string{"password=\"-----BEGIN OPENSSH PRIVATE KEY-----\n", "fixture-quoted-key\n", "-----END OPENSSH PRIVATE KEY-----\"\n"}, []string{"fixture-quoted-key"}},
		{"private-key", "", []string{"-----BEGIN OPENSSH PRIVATE KEY-----\n", "fixture-key-body\n", "-----END OPENSSH PRIVATE KEY-----\n"}, []string{"fixture-key-body"}},
		{"markdown", "", []string{"###sudo密码\n", "fixture-metadata-password\n"}, []string{"fixture-metadata-password"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document.Hosts = []config.Host{{ID: "output-fixture", Password: test.secret}}
			app.mu.Unlock()
			events := observeCommands(t, app)
			id, write := fixtureCommand(t, app)
			for index, chunk := range test.chunks {
				write(commandFixtureStep{Stdout: []byte(chunk)})
				// Independent stderr remains live while stdout holds a split
				// credential. This is also an event barrier for every chunk.
				marker := fmt.Sprintf("stderr-progress-%d", index)
				write(commandFixtureStep{Stderr: []byte(marker + "\n")})
				job := awaitCommandEvent(t, events, id, func(job outputJobWire) bool {
					for _, forbidden := range test.forbidden {
						if strings.Contains(job.Output+job.Message, forbidden) {
							t.Fatal("a partial credential escaped before command completion")
						}
					}
					return strings.Contains(job.Output+job.Message, marker)
				})
				if job.State != "running" {
					t.Fatal("fixture command finished before release")
				}
			}
			write(commandFixtureStep{Stdout: []byte("public-finished\n")})
			exit := 0
			write(commandFixtureStep{Exit: &exit})
			job := awaitCommandEvent(t, events, id, func(job outputJobWire) bool { return job.State == "succeeded" })
			if !strings.Contains(job.Output, "public-finished") {
				t.Fatal("redaction swallowed subsequent public output")
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(job.Output+job.Message, forbidden) {
					t.Fatal("credential remained in the final snapshot")
				}
			}
		})
	}
}
