package strategy

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestExecuteFallsThroughAndApprovesRiskOnce(t *testing.T) {
	t.Parallel()
	var order []string
	approvals := 0
	attempts := []Attempt{
		{Tier: Direct, Method: SCP, Risk: SudoRisk, Run: func(context.Context) error { order = append(order, "scp"); return errors.New("blocked") }},
		{Tier: Direct, Method: EncryptedStream, Risk: SudoRisk, Run: func(context.Context) error { order = append(order, "stream"); return nil }},
	}
	err := Execute(context.Background(), attempts, func(context.Context, Risk, Attempt) error { approvals++; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approvals != 1 || !reflect.DeepEqual(order, []string{"scp", "stream"}) {
		t.Fatalf("approvals=%d order=%v", approvals, order)
	}
}

func TestSortProbesMedianThenRecent(t *testing.T) {
	t.Parallel()
	now := time.Now()
	results := []ProbeResult{
		{Name: "slow", Successful: true, Samples: []time.Duration{30, 50, 40}},
		{Name: "failed", Successful: false, Samples: []time.Duration{1}},
		{Name: "fast", Successful: true, Samples: []time.Duration{20, 10, 15}, LastOK: now},
	}
	SortProbes(results)
	if results[0].Name != "fast" || results[2].Name != "failed" {
		t.Fatalf("unexpected order: %#v", results)
	}
}
