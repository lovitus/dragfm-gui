package gui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestFilterCWDMarkers(t *testing.T) {
	t.Parallel()
	var directory string
	input := "before\x1b]777;dragfm-cwd=v1;test-session;L3RtcC93b3Jr\x07after"
	output, err := io.ReadAll(endpoint.FilterCWDMarkers(strings.NewReader(input), "test-session", func(value string) { directory = value }))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "beforeafter" || directory != "/tmp/work" {
		t.Fatalf("output=%q directory=%q", output, directory)
	}
}

func TestFilterCWDMarkersDoesNotHoldPromptTail(t *testing.T) {
	t.Parallel()
	source, input := io.Pipe()
	defer input.Close()
	filtered := endpoint.FilterCWDMarkers(source, "test-session", func(string) {})
	defer filtered.Close()
	prompt := []byte("fanli@host /Users % ")
	go func() { _, _ = input.Write(prompt) }()
	result := make(chan []byte, 1)
	go func() {
		output := make([]byte, len(prompt))
		_, _ = io.ReadFull(filtered, output)
		result <- output
	}()
	select {
	case output := <-result:
		if string(output) != string(prompt) {
			t.Fatalf("output=%q want=%q", output, prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt tail was held waiting for another PTY write")
	}
}
