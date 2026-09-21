package endpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const FilesystemChildFlag = "--dragfm-filesystem-op"

// FilesystemChildMain implements narrowly scoped filesystem subprocesses before
// any GUI/vault initialization. It does not elevate itself. The parent invokes
// this entrypoint through approved sudo and feeds credentials and data via stdin.
func FilesystemChildMain(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != FilesystemChildFlag {
		return false, 0
	}
	fail := func(err error) (bool, int) {
		fmt.Fprintln(stderr, "filesystem operation:", err)
		code := 1
		if errors.Is(err, fs.ErrExist) {
			code = 73
		}
		if errors.Is(err, fs.ErrPermission) {
			code = 77
		}
		if errors.Is(err, fs.ErrNotExist) {
			code = 66
		}
		return true, code
	}
	if len(args) < 4 || len(args[2]) != 64 || !filepath.IsAbs(args[3]) {
		return fail(errors.New("invalid filesystem request"))
	}
	// sudo -S can consume the password or skip it (NOPASSWD/cached ticket).
	// A random non-credential frame marker lets us discard precisely the optional
	// password line without ever treating it as file data. No extra fd survives sudo.
	input := bufio.NewReaderSize(stdin, 8192)
	expected := "dragfm-fs-v1:" + args[2]
	line, err := input.ReadSlice('\n')
	if err != nil || len(line) > 4096 {
		return fail(errors.New("missing filesystem frame"))
	}
	if strings.TrimSuffix(string(line), "\n") != expected {
		line, err = input.ReadSlice('\n')
		if err != nil || strings.TrimSuffix(string(line), "\n") != expected {
			return fail(errors.New("invalid filesystem frame"))
		}
	}
	local := NewLocal()
	ctx := context.Background()
	path := args[3]
	switch args[1] {
	case "stat":
		entry, e := local.Stat(ctx, path)
		err = e
		if err == nil {
			err = json.NewEncoder(stdout).Encode(entry)
		}
	case "list":
		entries, e := local.List(ctx, path)
		err = e
		if err == nil {
			err = json.NewEncoder(stdout).Encode(entries)
		}
	case "readlink":
		target, e := local.Readlink(ctx, path)
		err = e
		if err == nil {
			err = json.NewEncoder(stdout).Encode(target)
		}
	case "read":
		var file *os.File
		file, err = os.Open(path)
		if err == nil {
			_, err = io.Copy(stdout, file)
			err = errors.Join(err, file.Close())
		}
	case "write":
		if err = rejectFilesystemRoot(path); err != nil {
			break
		}
		var file *os.File
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = io.Copy(file, input)
			err = errors.Join(err, file.Sync(), file.Close())
		}
	case "rename":
		if len(args) != 6 || !filepath.IsAbs(args[4]) || (args[5] != "true" && args[5] != "false") {
			err = errors.New("invalid rename request")
			break
		}
		if err = errors.Join(rejectFilesystemRoot(path), rejectFilesystemRoot(args[4])); err == nil {
			err = local.Rename(ctx, path, args[4], args[5] == "true")
		}
	case "times":
		if len(args) != 6 {
			err = errors.New("invalid timestamp request")
			break
		}
		at, aerr := strconv.ParseInt(args[4], 10, 64)
		mt, merr := strconv.ParseInt(args[5], 10, 64)
		err = errors.Join(aerr, merr)
		if err == nil {
			err = os.Chtimes(path, time.Unix(0, at), time.Unix(0, mt))
		}
	default:
		err = errors.New("unsupported filesystem operation")
	}
	if err != nil {
		return fail(err)
	}
	return true, 0
}
