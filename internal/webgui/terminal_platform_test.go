package webgui

import (
	"strings"
	"testing"
)

func TestPowerShellDirectoryUsesLiteralPathAndEscapesQuotes(t *testing.T) {
	command := powershellCDCommand("C:\\space dir\\it's [a] $x `test`")
	if command != "Set-Location -LiteralPath 'C:\\space dir\\it''s [a] $x `test`'\r" {
		t.Fatalf("unsafe PowerShell directory command: %q", command)
	}
	if !strings.HasSuffix(command, "\r") || strings.Contains(command, "printf") {
		t.Fatalf("wrong shell command: %q", command)
	}
}
