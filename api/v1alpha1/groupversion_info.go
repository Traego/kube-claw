// Package v1alpha1 contains the claw.run/v1alpha1 API types.
// kube-claw exposes a single CRD (Agent); all other state is controller-owned
// (see DESIGN.md §6, §7).
//
// +kubebuilder:object:generate=true
// +groupName=claw.run
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the claw.run/v1alpha1 group-version.
	GroupVersion = schema.GroupVersion{Group: "claw.run", Version: "v1alpha1"}

	// SchemeBuilder registers the API types with a runtime.Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the claw.run/v1alpha1 types to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
