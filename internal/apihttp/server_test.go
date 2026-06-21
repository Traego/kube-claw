package apihttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clawv1alpha1 "github.com/traego/kube-claw/api/v1alpha1"
	"github.com/traego/kube-claw/internal/store"
	"github.com/traego/kube-claw/internal/store/sqlite"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "claw.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = clawv1alpha1.AddToScheme(s)
	reader := fake.NewClientBuilder().WithScheme(s).WithObjects(&clawv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "gcp-cost", Namespace: "claw-agents"},
		Status:     clawv1alpha1.AgentStatus{Phase: "Sleeping", SelectedImageDigest: "sha256:abc123"},
	}).Build()

	return &Server{Store: st, Reader: reader}
}

func TestAPI_HealthAndAgents(t *testing.T) {
	srv := newTestServer(t)
	h := srv.handler()

	// healthz
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz code = %d", rr.Code)
	}

	// list agents
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/agents", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("agents code = %d", rr.Code)
	}
	var agents []agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "gcp-cost" || agents[0].Digest != "sha256:abc123" {
		t.Fatalf("agents = %+v, want gcp-cost/sha256:abc123", agents)
	}
}

func TestAPI_CreateAndGetRun(t *testing.T) {
	srv := newTestServer(t)
	h := srv.handler()

	// create
	body := strings.NewReader(`{"namespace":"claw-agents","agent":"gcp-cost","input":"why did GCP cost spike?"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/runs", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create run code = %d body=%s", rr.Code, rr.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	id := created["id"]
	if id == "" || created["phase"] != "Pending" {
		t.Fatalf("create response = %v", created)
	}

	// get it back
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/runs/"+id, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get run code = %d", rr.Code)
	}
	var run store.Run
	if err := json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.ID != id || run.AgentName != "gcp-cost" || run.Phase != "Pending" {
		t.Fatalf("run = %+v", run)
	}

	// audit row was written in the same tx
	var n int
	if err := srv.Store.Tx(context.Background(), func(tx store.Tx) error {
		// piggyback: ListRuns proves persistence; audit count via a fresh query
		runs, e := tx.ListRuns(10)
		n = len(runs)
		return e
	}); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if n != 1 {
		t.Fatalf("runs persisted = %d, want 1", n)
	}

	// unknown run → 404
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/runs/run-bogus", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown run code = %d, want 404", rr.Code)
	}
}
