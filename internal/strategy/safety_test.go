package strategy

import (
	"context"
	"errors"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"testing"
)

func TestSafetyFailureNeverRetriesAgainstANewSourceSnapshot(t *testing.T) {
	for _, safetyErr := range []error{transfer.ErrSourceChanged, transfer.PreserveSource(errors.New("target verification failed"))} {
		retried := false
		err := Execute(context.Background(), []Attempt{
			{Run: func(context.Context) error { return errors.Join(errors.New("copied but not moved"), safetyErr) }},
			{Run: func(context.Context) error { retried = true; return nil }},
		}, nil, nil)
		if err == nil || retried {
			t.Fatalf("safety failure was retried: %v retried=%v", err, retried)
		}
	}
}
