// Package apihttp serves the controller's HTTP API (DESIGN.md §13).
//
// Phase 2 testability slice: enough to smoke-test a deployed controller —
//
//	GET  /healthz        liveness
//	GET  /v1/agents      list Agents (proves the k8s client + reconciler)
//	POST /v1/runs        create a run (proves the SQLite write path + audit)
//	GET  /v1/runs/{id}   read it back (proves persistence on the PVC)
//
// Auth (SA-token / claw session token) and TLS are layered in later phases; for
// now this is a cluster-internal, port-forwarded surface.
package apihttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/traego/kube-claw/api/v1alpha1"
	"github.com/traego/kube-claw/internal/store"
)

// Server is a controller-runtime Runnable that serves the HTTP API.
type Server struct {
	Addr   string
	Store  store.Store
	Reader client.Reader // uncached k8s reader (mgr.GetAPIReader)
}

// NeedLeaderElection lets the API run on every replica (false = not gated).
func (s *Server) NeedLeaderElection() bool { return false }

// Start runs the HTTP server until ctx is cancelled (manager.Runnable).
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	logf.Log.WithName("apihttp").Info("serving HTTP API", "addr", s.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/agents", s.listAgents)
	mux.HandleFunc("POST /v1/runs", s.createRun)
	mux.HandleFunc("GET /v1/runs/{id}", s.getRun)
	mux.HandleFunc("POST /v1/runs/{id}/outputs", s.postOutput)
	return mux
}

type agentView struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Digest    string `json:"digest"`
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	var list clawv1alpha1.AgentList
	if err := s.Reader.List(r.Context(), &list); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]agentView, 0, len(list.Items))
	for _, a := range list.Items {
		out = append(out, agentView{a.Namespace, a.Name, a.Status.Phase, a.Status.SelectedImageDigest})
	}
	writeJSON(w, http.StatusOK, out)
}

type createRunReq struct {
	Namespace string `json:"namespace"`
	Agent     string `json:"agent"`
	Input     string `json:"input"`
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var req createRunReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Agent == "" || req.Namespace == "" {
		writeErr(w, http.StatusBadRequest, "namespace and agent are required")
		return
	}
	run := store.Run{
		ID:             newRunID(),
		AgentNamespace: req.Namespace,
		AgentName:      req.Agent,
		Phase:          "Pending",
		Input:          mustJSON(map[string]string{"text": req.Input}),
		Source:         mustJSON(map[string]string{"trigger": "cli"}),
		CreatedAt:      store.NowRFC3339(),
	}
	if err := s.Store.Tx(r.Context(), func(tx store.Tx) error {
		if err := tx.CreateRun(run); err != nil {
			return err
		}
		// Audit in the same tx (the store invariant).
		return tx.AppendAudit(store.AuditEvent{
			Type:  "agentrun.created",
			RunID: run.ID,
			Actor: "cli",
			Detail: map[string]any{
				"agent":     req.Agent,
				"namespace": req.Namespace,
			},
		})
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": run.ID, "phase": run.Phase})
}

type runView struct {
	store.Run
	Outputs []store.Output `json:"outputs"`
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var view runView
	err := s.Store.Tx(r.Context(), func(tx store.Tx) error {
		run, e := tx.GetRun(id)
		if e != nil {
			return e
		}
		outs, e := tx.ListOutputs(id)
		if e != nil {
			return e
		}
		view = runView{Run: run, Outputs: outs}
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if view.Outputs == nil {
		view.Outputs = []store.Output{}
	}
	writeJSON(w, http.StatusOK, view)
}

type postOutputReq struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// postOutput records a runner's output and marks the run Succeeded. This is the
// runner→controller callback (DESIGN.md §36). Auth (claw session token) lands in
// Phase 5; for now it is unauthenticated on the cluster-internal API.
func (s *Server) postOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req postOutputReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Kind == "" {
		req.Kind = "text"
	}
	err := s.Store.Tx(r.Context(), func(tx store.Tx) error {
		if _, e := tx.GetRun(id); e != nil {
			return e
		}
		if e := tx.AppendOutput(id, store.Output{Kind: req.Kind, Content: req.Content}); e != nil {
			return e
		}
		if e := tx.MarkRunSucceeded(id); e != nil {
			return e
		}
		return tx.AppendAudit(store.AuditEvent{Type: "agentrun.completed", RunID: id, Actor: "runner"})
	})
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// --- helpers ---

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "run-" + hex.EncodeToString(b)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
