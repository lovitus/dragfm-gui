package transfer

import (
	"context"
	"errors"
	"fmt"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"io/fs"
)

func ensureCopyDirectory(ctx context.Context, destination endpoint.Endpoint, target string) error {
	entry, err := destination.Stat(ctx, target)
	if err == nil {
		if !entry.IsDir() {
			return PreserveSource(fmt.Errorf("copy directory %q is not a real directory", target))
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return destination.MkdirAll(ctx, target, 0700)
}

func verifyCopyParents(ctx context.Context, operation Operation, target string) error {
	root := operation.TargetPath
	if target == root {
		return nil
	}
	for parent := operation.Destination.Dir(target); ; parent = operation.Destination.Dir(parent) {
		entry, err := operation.Destination.Stat(ctx, parent)
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return PreserveSource(fmt.Errorf("refusing copy through non-directory parent %q", parent))
		}
		if parent == root {
			return nil
		}
		if parent == operation.Destination.Dir(parent) {
			return PreserveSource(errors.New("copy path escaped destination"))
		}
	}
}
