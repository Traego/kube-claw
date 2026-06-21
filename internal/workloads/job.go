// Package workloads builds the Kubernetes objects that execute agent runs.
//
// Phase 5 (demo slice): a one-shot Job runs the generic claw-runner, which posts
// a response back to the controller. The full path (agent image + claw-bootstrap
// + /login + secret materialization) layers on top in later work — this proves
// the trigger→pod→response loop without secrets.
package workloads

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/traego/kube-claw/internal/store"
)

func mustQty(s string) resource.Quantity { return resource.MustParse(s) }

// RunJobName is the deterministic Job name for a run (idempotent creation).
func RunJobName(run store.Run) string { return run.ID }

// BuildRunJob builds the one-shot Job for a run. It runs as the agent's
// ServiceAccount (claw-agent-<name>) with a locked-down pod security context.
func BuildRunJob(run store.Run, runnerImage, controllerURL, inputText string) *batchv1.Job {
	backoff := int32(1)
	ttl := int32(600)
	deadline := int64(120)
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "kube-claw",
		"claw.run/agent":               run.AgentName,
		"claw.run/run-id":              run.ID,
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RunJobName(run),
			Namespace: run.AgentNamespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName: "claw-agent-" + run.AgentName,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   ptr(true),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:  "runner",
						Image: runnerImage,
						Env: []corev1.EnvVar{
							{Name: "CLAW_RUN_ID", Value: run.ID},
							{Name: "CLAW_AGENT_NAME", Value: run.AgentName},
							{Name: "CLAW_AGENT_NAMESPACE", Value: run.AgentNamespace},
							{Name: "CLAW_CONTROLLER_URL", Value: controllerURL},
							{Name: "CLAW_INPUT", Value: inputText},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr(false),
							ReadOnlyRootFilesystem:   ptr(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    mustQty("100m"),
								corev1.ResourceMemory: mustQty("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    mustQty("500m"),
								corev1.ResourceMemory: mustQty("256Mi"),
							},
						},
					}},
				},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }
