package transfer

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func physicalOperationPaths(ctx context.Context, op Operation, source, target string) (string, string, error) {
	type resolver interface { PhysicalPath(context.Context, string) (string, error) }
	for _, item := range []struct { endpoint endpoint.Endpoint; value *string }{{op.Source, &source}, {op.Destination, &target}} {
		if physical, ok := item.endpoint.(resolver); ok {
			value, err := physical.PhysicalPath(ctx, *item.value)
			if err != nil { return "", "", fmt.Errorf("resolve directory aliases safely: %w", err) }
			*item.value = value
		}
	}
	// Windows paths are case-insensitive on the supported local filesystem.
	// Both endpoints here have already been proven to be the same machine.
	if runtime.GOOS == "windows" {
		identity, _ := op.Source.Identity(ctx)
		if identity.Kind == endpoint.LocalKind { source, target = strings.ToLower(source), strings.ToLower(target) }
	}
	return source, target, nil
}
