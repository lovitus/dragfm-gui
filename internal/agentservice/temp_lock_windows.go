package agentservice

import (
	"errors"
	"os"
)

var errDirectoryLeased = errors.New("temporary directory lease cannot be verified on this platform")

func effectiveOwner(os.FileInfo) bool               { return false }
func lockStaleDirectory(*os.Root) (*os.File, error) { return nil, errDirectoryLeased }
