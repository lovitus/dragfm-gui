package endpoint

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"
)

func TestMissingOrMalformedRemoteIdentityDoesNotInventSameMachine(t *testing.T) {
	for _, value := range []string{"", "not-a-machine-id", strings.Repeat("a", 4096), strings.Repeat("0", 32)} {
		id, err := readMachineID(context.Background(), func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(value)), nil
		})
		if err != nil || id != "" {
			t.Fatalf("invalid identity accepted: %q %v", id, err)
		}
	}
	id, err := readMachineID(context.Background(), func(context.Context, string) (io.ReadCloser, error) { return nil, fs.ErrNotExist })
	if id != "" || err != nil {
		t.Fatalf("machine without ID files: %q %v", id, err)
	}
}

func TestRemoteMachineIDBoundedReadAndFallback(t *testing.T) {
	calls := 0
	id, err := readMachineID(context.Background(), func(_ context.Context, path string) (io.ReadCloser, error) {
		calls++
		if path == "/etc/machine-id" {
			return nil, fs.ErrPermission
		}
		return io.NopCloser(strings.NewReader("ABCDEF0123456789ABCDEF0123456789\n")), nil
	})
	if err != nil || id != "abcdef0123456789abcdef0123456789" || calls != 2 {
		t.Fatalf("fallback: %q %v calls=%d", id, err, calls)
	}
}
