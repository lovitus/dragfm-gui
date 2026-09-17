//go:build windows

package endpoint

import "errors"

func localFileVersion(string) (uint64, uint64, error) {
	return 0, 0, errors.New("Windows file identity is unavailable")
}
