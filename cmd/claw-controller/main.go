// Command claw-controller is the kube-claw control plane: Kubernetes operator,
// secret authority, embedded Slack router, and workload API (DESIGN.md §4).
//
// Phase 0 wires the controller-runtime manager + scheme + health probes.
// Reconcilers, the store, the secret authority, and the API land in later phases.
package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clawv1alpha1 "github.com/traego/kube-claw/api/v1alpha1"
	"github.com/traego/kube-claw/internal/apihttp"
	"github.com/traego/kube-claw/internal/controller"
	"github.com/traego/kube-claw/internal/runengine"
	"github.com/traego/kube-claw/internal/store/sqlite"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clawv1alpha1.AddToScheme(scheme))
}

func main() {
	var dataDir, probeAddr, apiAddr, runnerImage, selfURL string
	var enableRouter bool
	flag.StringVar(&dataDir, "data-dir", "/var/lib/claw", "directory for the SQLite store")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe bind address")
	flag.StringVar(&apiAddr, "api-bind-address", ":8443", "HTTP API bind address")
	flag.StringVar(&runnerImage, "runner-image", "claw-runner:dev", "image used for agent run Jobs")
	flag.StringVar(&selfURL, "self-url", "http://claw-controller.claw-system.svc:8443", "in-cluster URL run pods use to reach the controller")
	flag.BoolVar(&enableRouter, "enable-router", true, "run the embedded Slack router")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := (&controller.AgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to set up AgentReconciler")
		os.Exit(1)
	}

	// Open the SQLite store on the PVC and migrate.
	st, err := sqlite.Open(context.Background(), filepath.Join(dataDir, "claw.db"))
	if err != nil {
		log.Error(err, "unable to open store", "dataDir", dataDir)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		log.Error(err, "unable to migrate store")
		os.Exit(1)
	}

	// HTTP API (uncached reader so /v1/agents works without waiting on caches).
	if err := mgr.Add(&apihttp.Server{
		Addr:   apiAddr,
		Store:  st,
		Reader: mgr.GetAPIReader(),
	}); err != nil {
		log.Error(err, "unable to add HTTP API server")
		os.Exit(1)
	}

	// Run engine: launches a Job per Pending run (Phase 5 demo slice).
	if err := mgr.Add(&runengine.Engine{
		Store:         st,
		K8s:           mgr.GetClient(),
		RunnerImage:   runnerImage,
		ControllerURL: selfURL,
		Interval:      2 * time.Second,
	}); err != nil {
		log.Error(err, "unable to add run engine")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to add healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to add readyz check")
		os.Exit(1)
	}

	// dataDir / enableRouter are consumed by the store + router in later phases.
	_ = dataDir
	_ = enableRouter

	log.Info("starting claw-controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited with error")
		os.Exit(1)
	}
}
