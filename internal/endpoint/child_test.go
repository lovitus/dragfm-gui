package endpoint

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if handled, code := FilesystemChildMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
