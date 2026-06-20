// Command claw-controller is the kube-claw control plane: Kubernetes operator,
// secret authority, embedded Slack router, and workload API (DESIGN.md §4).
//
// Phase 0 wires the controller-runtime manager + scheme + health probes.
// Reconcilers, the store, the secret authority, and the API land in later phases.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clawv1alpha1 "github.com/traego/kube-claw/api/v1alpha1"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clawv1alpha1.AddToScheme(scheme))
}

func main() {
	var dataDir, probeAddr string
	var enableRouter bool
	flag.StringVar(&dataDir, "data-dir", "/var/lib/claw", "directory for the SQLite store")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe bind address")
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
