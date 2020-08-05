package main

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	RegisterPodDefault("minio-profile", func(profile *kubeflowv1.Profile) *kubeflowv1alpha1.PodDefault {
		roleName := cleanName(fmt.Sprintf("profile-%s", profile.Name))

		return &kubeflowv1alpha1.PodDefault{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "minio-profile",
				Namespace: profile.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
				},
			},
			Spec: kubeflowv1alpha1.PodDefaultSpec{
				Desc: "Inject credentials to access MinIO object storage",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"minio-profile": "true",
					},
				},
				Annotations: map[string]string{
					"vault.hashicorp.com/agent-inject":                              "true",
					"vault.hashicorp.com/agent-pre-populate":                        "false",
					"vault.hashicorp.com/role":                                      roleName,
					"vault.hashicorp.com/agent-inject-secret-minio-minimal-tenant1": "minio_minimal_tenant1/keys/" + roleName,
					"vault.hashicorp.com/agent-inject-template-minio-minimal-tenant1": fmt.Sprintf(`
{{- with secret "minio_minimal_tenant1/keys/%s" }}
export MINIO_URL="http://minimal-tenant1-minio.minio:9000"
export MINIO_ACCESS_KEY="{{ .Data.accessKeyId }}"
export MINIO_SECRET_KEY="{{ .Data.secretAccessKey }}"
{{- end }}
					`, roleName),
					"vault.hashicorp.com/agent-inject-secret-minio-pachyderm-tenant1": "minio_pachyderm_tenant1/keys/" + roleName,
					"vault.hashicorp.com/agent-inject-template-minio-pachyderm-tenant1": fmt.Sprintf(`
{{- with secret "minio_pachyderm_tenant1/keys/%s" }}
export MINIO_URL="http://pachyderm-tenant1-minio.minio:9000"
export MINIO_ACCESS_KEY="{{ .Data.accessKeyId }}"
export MINIO_SECRET_KEY="{{ .Data.secretAccessKey }}"
{{- end }}
					`, roleName),
					"vault.hashicorp.com/agent-inject-secret-minio-premium-tenant1": "minio_premium_tenant1/keys/" + roleName,
					"vault.hashicorp.com/agent-inject-template-minio-premium-tenant1": fmt.Sprintf(`
{{- with secret "minio_premium_tenant1/keys/%s" }}
export MINIO_URL="http://premium-tenant1-minio.minio:9000"
export MINIO_ACCESS_KEY="{{ .Data.accessKeyId }}"
export MINIO_SECRET_KEY="{{ .Data.secretAccessKey }}"
{{- end }}
					`, roleName),
				},
			},
		}
	})
}
