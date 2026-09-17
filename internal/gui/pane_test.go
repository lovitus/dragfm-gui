package gui

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestFilterCWDMarkers(t *testing.T) {
	t.Parallel()
	var directory string
	input := "before\x1b]777;dragfm-cwd=/tmp/work\x07after"
	output, err := io.ReadAll(filterCWDMarkers(strings.NewReader(input), func(value string) { directory = value }))
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
	filtered := filterCWDMarkers(source, func(string) {})
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
