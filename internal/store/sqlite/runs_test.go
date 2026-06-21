package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/traego/kube-claw/internal/store"
)

// TestRunLifecycle walks a run through the demo loop: created (Pending) →
// picked up (Running) → output recorded (Succeeded), with outputs persisted.
func TestRunLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "claw.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const id = "run-abc"
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}

	// create Pending
	must(s.Tx(ctx, func(tx store.Tx) error {
		return tx.CreateRun(store.Run{ID: id, AgentNamespace: "claw-agents", AgentName: "gcp-cost", Phase: "Pending"})
	}))

	// engine picks up Pending (FIFO)
	must(s.Tx(ctx, func(tx store.Tx) error {
		runs, err := tx.ListRunsByPhase("Pending", 10)
		if err != nil {
			return err
		}
		if len(runs) != 1 || runs[0].ID != id {
			t.Fatalf("pending = %+v", runs)
		}
		return tx.MarkRunRunning(id, "run-abc")
	}))

	// no longer Pending
	must(s.Tx(ctx, func(tx store.Tx) error {
		runs, err := tx.ListRunsByPhase("Pending", 10)
		if err != nil {
			return err
		}
		if len(runs) != 0 {
			t.Fatalf("still pending: %+v", runs)
		}
		return nil
	}))

	// runner posts output → Succeeded
	must(s.Tx(ctx, func(tx store.Tx) error {
		if err := tx.AppendOutput(id, store.Output{Kind: "text", Content: "demo response"}); err != nil {
			return err
		}
		return tx.MarkRunSucceeded(id)
	}))

	// verify final state + output
	must(s.Tx(ctx, func(tx store.Tx) error {
		run, err := tx.GetRun(id)
		if err != nil {
			return err
		}
		if run.Phase != "Succeeded" || run.AssignedPod != "run-abc" || run.CompletedAt == "" {
			t.Fatalf("final run = %+v", run)
		}
		outs, err := tx.ListOutputs(id)
		if err != nil {
			return err
		}
		if len(outs) != 1 || outs[0].Content != "demo response" {
			t.Fatalf("outputs = %+v", outs)
		}
		return nil
	}))
}
