package main

import (
	"fmt"

	kubeflowv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
	kubeflowv1alpha1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1alpha1"
)

// NewPodDefaultFunc represents the function called to create a new PodDefault.
type NewPodDefaultFunc func(profile *kubeflowv1.Profile) *kubeflowv1alpha1.PodDefault

var (
	// PodDefaults contains the map of registered PodDefaults.
	PodDefaults = make(map[string]NewPodDefaultFunc)
)

// RegisterPodDefault registers a new PodDefault.
// NOTE: The object name returned MUST match the registered name.
func RegisterPodDefault(name string, callback NewPodDefaultFunc) error {
	if _, ok := PodDefaults[name]; ok {
		return fmt.Errorf("PodDefault %q is already registered", name)
	}

	PodDefaults[name] = callback
	return nil
}

func init() {
	RegisterPodDefault("minio-mounts", func(profile *kubeflowv1.Profile) *kubeflowv1alpha1.PodDefault {
		return &kubeflowv1alpha1.PodDefault{}
	})
}
