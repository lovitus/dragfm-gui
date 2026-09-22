//go:build windows

package agentservice

import "os"

// Helpers only run on Linux. Never infer Unix-style ownership on Windows.
func sameOwner(a, b os.FileInfo) bool { return false }
