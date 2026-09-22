//go:build !windows

package endpoint

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
