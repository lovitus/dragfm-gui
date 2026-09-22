package webgui

import (
	"strings"
	"testing"
)

func TestBareMasterPasswordIsRedactedFromHistory(t *testing.T) {
	app := &App{password: []byte("unique-master-secret-value")}
	value := app.redactKnownLocked("command output: unique-master-secret-value\nnext line")
	if strings.Contains(value, string(app.password)) || !strings.Contains(value, "\nnext line") {
		t.Fatalf("master password not safely redacted: %q", value)
	}
}
