//go:build windows

package agentservice

import "os"

func filesystemVersion(os.FileInfo) (uint64, uint64) { return 0, 0 }
