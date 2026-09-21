//go:build !windows

package endpoint

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSudoAtomicWriterKeepsCredentialAndDataStreamsSeparate(t *testing.T) {
	for _, consume := range []bool{true, false} {
		t.Run(fmt.Sprint("sudo-consumes-password=", consume), func(t *testing.T) {
			bin := t.TempDir()
			fakeSudo := filepath.Join(bin, "sudo")
			script := `#!/bin/sh
password_stdin=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    -S) password_stdin=true; shift ;;
    -n) shift ;;
    -p) shift 2 ;;
    --) shift; break ;;
    *) break ;;
  esac
done
if [ "$password_stdin" = true ]; then
  IFS= read -r ignored_password
fi
exec 3<&-
exec "$@"
`
			if !consume {
				script = strings.Replace(script, `if [ "$password_stdin" = true ]; then`, `if false; then`, 1)
			}
			if err := os.WriteFile(fakeSudo, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			root := t.TempDir()
			target := filepath.Join(root, "target.txt")
			elevated := &SudoLocal{local: NewLocal(), password: "must-not-enter-file"}
			writer, err := elevated.CreateAtomic(context.Background(), target, 0640)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(writer, "payload-only"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Commit(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "payload-only" {
				t.Fatalf("credential leaked into file data: %q", data)
			}
			if matches, err := filepath.Glob(filepath.Join(root, ".dragfm-partial-*")); err != nil || len(matches) != 0 {
				t.Fatalf("partial files remain after commit: %v, %v", matches, err)
			}
		})
	}
}
