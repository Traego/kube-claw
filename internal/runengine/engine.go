// Package runengine processes runs from the store: it watches for Pending runs
// and launches a Job per run (DESIGN.md §22, §31 run engine).
//
// Phase 5 demo slice: no secrets/approval gating yet — a Pending run launches a
// Job immediately. The approval gate (run → Blocked until a grant exists) slots
// in front of job creation in Phase 4.
package runengine

import (
	"context"
	"encoding/json"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traego/kube-claw/internal/store"
	"github.com/traego/kube-claw/internal/workloads"
)

// Engine launches Jobs for Pending runs on an interval.
type Engine struct {
	Store         store.Store
	K8s           client.Client
	RunnerImage   string
	ControllerURL string
	Interval      time.Duration
}

// NeedLeaderElection ensures only the leader processes runs (single-writer).
func (e *Engine) NeedLeaderElection() bool { return true }

// Start runs the processing loop until ctx is cancelled (manager.Runnable).
func (e *Engine) Start(ctx context.Context) error {
	if e.Interval <= 0 {
		e.Interval = 2 * time.Second
	}
	lg := logf.Log.WithName("runengine")
	lg.Info("starting run engine", "interval", e.Interval, "runnerImage", e.RunnerImage)
	t := time.NewTicker(e.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := e.processPending(ctx); err != nil {
				lg.Error(err, "processing pending runs")
			}
		}
	}
}

func (e *Engine) processPending(ctx context.Context) error {
	var pending []store.Run
	if err := e.Store.Tx(ctx, func(tx store.Tx) error {
		runs, err := tx.ListRunsByPhase("Pending", 20)
		pending = runs
		return err
	}); err != nil {
		return err
	}
	for _, run := range pending {
		e.launch(ctx, run)
	}
	return nil
}

func (e *Engine) launch(ctx context.Context, run store.Run) {
	lg := logf.Log.WithName("runengine").WithValues("run", run.ID, "agent", run.AgentName)

	job := workloads.BuildRunJob(run, e.RunnerImage, e.ControllerURL, inputText(run.Input))
	if err := e.K8s.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		lg.Error(err, "create job")
		return
	}

	if err := e.Store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.MarkRunRunning(run.ID, workloads.RunJobName(run)); err != nil {
			return err
		}
		return tx.AppendAudit(store.AuditEvent{
			Type:  "agentrun.started",
			RunID: run.ID,
			Actor: "runengine",
			Detail: map[string]any{
				"job":   workloads.RunJobName(run),
				"agent": run.AgentName,
			},
		})
	}); err != nil {
		lg.Error(err, "mark run running")
		return
	}
	lg.Info("launched run job", "job", workloads.RunJobName(run))
}

// inputText pulls the human text out of a run's Input JSON ({"text":"..."}).
func inputText(input string) string {
	var in struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(input), &in)
	return in.Text
}
