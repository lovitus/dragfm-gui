package gui

import (
	"strings"
	"testing"
)

func TestRedactCredentials(t *testing.T) {
	t.Parallel()
	raw := `flyssh uhome:"se@cret"@host --password second password=third`
	got := redact(raw)
	for _, secret := range []string{"se@cret", "second", "third"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redaction leaked %q in %q", secret, got)
		}
	}
}
