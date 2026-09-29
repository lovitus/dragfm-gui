package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// destinationGuard retains initial namespace evidence through copy, metadata
// restoration and final synchronization. Rechecking is read-only: an empty
// directory restored to 0555 must not need a second temporary write or sudo.
// It detects rebinding in those long phases, not every possible external race
// in the final interval between verification and removal.
type destinationGuard struct {
	sourceRoot, targetRoot string
	physicalTarget         string
	sourceDirectories      map[string]fileBinding
	targetDirectories      map[string]fileBinding
	publication            []string
}

func physicalTarget(ctx context.Context, op Operation) (string, error) {
	if resolver, ok := op.Destination.(interface {
		PhysicalPath(context.Context, string) (string, error)
	}); ok {
		return resolver.PhysicalPath(ctx, op.TargetPath)
	}
	return op.TargetPath, nil
}

func prepareDestinationGuard(ctx context.Context, op Operation, source Manifest) (*destinationGuard, error) {
	physical, err := physicalTarget(ctx, op)
	if err != nil {
		return nil, accessError(err, false, op.TargetPath)
	}
	guard := &destinationGuard{sourceRoot: op.SourcePath, targetRoot: op.TargetPath, physicalTarget: physical,
		sourceDirectories: make(map[string]fileBinding), targetDirectories: make(map[string]fileBinding)}
	// Remember exactly the missing publication chain and its first existing
	// parent, before a backend can create them. Unmodified ancestors above this
	// boundary need no fsync and may legitimately be searchable but unreadable.
	for parent := op.Destination.Dir(physical); ; parent = op.Destination.Dir(parent) {
		guard.publication = append(guard.publication, parent)
		entry, err := op.Destination.Stat(ctx, parent)
		if err == nil {
			if !entry.IsDir() {
				return nil, PreserveSource(fmt.Errorf("publication parent is not a real directory: %q", parent))
			}
			binding, err := bindingAt(ctx, op.Destination, parent)
			if err != nil {
				return nil, accessError(err, false, parent)
			}
			guard.targetDirectories[parent] = binding
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || parent == op.Destination.Dir(parent) {
			return nil, accessError(err, false, parent)
		}
	}
	for _, item := range source.Items {
		if item.Mode.IsDir() {
			binding, err := bindingAt(ctx, op.Source, item.SourcePath)
			if err != nil {
				return nil, accessError(err, true, item.SourcePath)
			}
			guard.sourceDirectories[item.SourcePath] = binding
		}
	}
	if op.Baseline != nil && op.Baseline.destination != nil {
		previous := op.Baseline.destination
		if err := previous.verify(ctx, op); err != nil {
			return nil, err
		}
		// A failed previous method may already have created publication
		// parents. Keep the original boundary; never silently shrink it.
		guard.publication = append([]string(nil), previous.publication...)
		for path, binding := range previous.targetDirectories {
			guard.targetDirectories[path] = binding
		}
	}
	return guard, nil
}

func (g *destinationGuard) verify(ctx context.Context, op Operation) error {
	if op.SourcePath != g.sourceRoot || op.TargetPath != g.targetRoot {
		return PreserveSource(errors.New("transfer paths no longer match the initial operation"))
	}
	physical, err := physicalTarget(ctx, op)
	if err != nil {
		return PreserveSource(accessError(err, false, op.TargetPath))
	}
	if physical != g.physicalTarget {
		return PreserveSource(errors.New("目标父路径在传输期间改变，源已保留"))
	}
	for _, side := range []struct {
		ep       endpoint.Endpoint
		bindings map[string]fileBinding
		source   bool
	}{{op.Source, g.sourceDirectories, true}, {op.Destination, g.targetDirectories, false}} {
		for path, expected := range side.bindings {
			actual, err := bindingAt(ctx, side.ep, path)
			if err != nil {
				return PreserveSource(accessError(err, side.source, path))
			}
			if actual != expected {
				return PreserveSource(fmt.Errorf("目录在传输期间被替换，源已保留: %q", path))
			}
		}
	}
	return nil
}
