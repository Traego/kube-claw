package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clawv1alpha1 "github.com/traego/kube-claw/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := clawv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newAgent() *clawv1alpha1.Agent {
	return &clawv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "gcp-cost", Namespace: "claw-agents"},
		Spec: clawv1alpha1.AgentSpec{
			Image: "ghcr.io/example/claw/gcp-cost@sha256:abc123",
			Storage: clawv1alpha1.StorageSpec{
				Workspace: &clawv1alpha1.VolumeSpec{Type: "pvc", Size: "10Gi", MountPath: "/workspace"},
				Memory:    &clawv1alpha1.VolumeSpec{Type: "pvc", Size: "5Gi", MountPath: "/memory"},
				Cache:     &clawv1alpha1.VolumeSpec{Type: "emptyDir", MountPath: "/cache"},
			},
		},
	}
}

func reconcileOnce(t *testing.T, objs ...client.Object) (client.Client, *clawv1alpha1.Agent) {
	t.Helper()
	s := testScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&clawv1alpha1.Agent{}).
		Build()
	r := &AgentReconciler{Client: c, Scheme: s}

	key := types.NamespacedName{Name: objs[0].GetName(), Namespace: objs[0].GetNamespace()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got clawv1alpha1.Agent
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return c, &got
}

func TestReconcile_HappyPath(t *testing.T) {
	c, got := reconcileOnce(t, newAgent())
	ctx := context.Background()

	// Status: digest + spec hash + Ready.
	if got.Status.SelectedImageDigest != "sha256:abc123" {
		t.Errorf("digest = %q, want sha256:abc123", got.Status.SelectedImageDigest)
	}
	if got.Status.AgentSpecHash == "" {
		t.Error("agentSpecHash is empty")
	}
	if got.Status.Phase != "Sleeping" {
		t.Errorf("phase = %q, want Sleeping", got.Status.Phase)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, "Ready"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("Ready condition = %v, want True", cond)
	}

	// ServiceAccount created, with NO RBAC (we don't create any RoleBinding here).
	var sa corev1.ServiceAccount
	if err := c.Get(ctx, types.NamespacedName{Name: "claw-agent-gcp-cost", Namespace: "claw-agents"}, &sa); err != nil {
		t.Fatalf("service account not created: %v", err)
	}

	// PVCs created for workspace + memory; NOT for the emptyDir cache.
	for _, n := range []string{"gcp-cost-workspace", "gcp-cost-memory"} {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, types.NamespacedName{Name: n, Namespace: "claw-agents"}, &pvc); err != nil {
			t.Fatalf("pvc %s not created: %v", n, err)
		}
	}
	var cachePVC corev1.PersistentVolumeClaim
	if err := c.Get(ctx, types.NamespacedName{Name: "gcp-cost-cache", Namespace: "claw-agents"}, &cachePVC); !apierrors.IsNotFound(err) {
		t.Errorf("cache PVC should not exist (emptyDir), got err=%v", err)
	}

	// NetworkPolicy created.
	var np networkingv1.NetworkPolicy
	if err := c.Get(ctx, types.NamespacedName{Name: "gcp-cost-agent", Namespace: "claw-agents"}, &np); err != nil {
		t.Fatalf("network policy not created: %v", err)
	}
}

func TestReconcile_InvalidImage(t *testing.T) {
	a := newAgent()
	a.Spec.Image = "ghcr.io/example/claw/gcp-cost:latest" // tag, not a digest
	_, got := reconcileOnce(t, a)

	if got.Status.Phase != "Failed" {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, "Ready")
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "InvalidImage" {
		t.Errorf("Ready condition = %v, want False/InvalidImage", cond)
	}
}

func TestSpecHash_StableAndSensitive(t *testing.T) {
	a := newAgent()
	h1 := specHash(&a.Spec)
	h2 := specHash(&a.Spec)
	if h1 != h2 {
		t.Errorf("spec hash not stable: %s vs %s", h1, h2)
	}
	a.Spec.Image = "ghcr.io/example/claw/gcp-cost@sha256:def456"
	if specHash(&a.Spec) == h1 {
		t.Error("spec hash did not change after image change")
	}
}
