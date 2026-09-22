//go:build !linux && !darwin && !windows

package filecommit

import "errors"

func NoReplace(source, target string) error {
	return errors.New("atomic no-replace rename is unavailable on this platform; source retained")
}
