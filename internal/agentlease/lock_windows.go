//go:build windows

package agentlease

import (
	"errors"
	"os"
)

func lock(*os.File, bool) error {
	return errors.New("uploaded helper directory leases require a Unix endpoint")
}
