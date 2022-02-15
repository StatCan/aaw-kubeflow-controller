package main

import (
	"fmt"

	kubeflowv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
	kubeflowv1alpha1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		return &kubeflowv1alpha1.PodDefault{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "minio-mounts",
				Namespace: profile.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
				},
			},
			Spec: kubeflowv1alpha1.PodDefaultSpec{
				Desc: "Mount MinIO storage into the minio/ folder",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"minio-mounts": "true",
					},
				},
				Annotations: map[string]string{
					"data.statcan.gc.ca/inject-boathouse": "true",
				},
			},
		}
	})
	RegisterPodDefault("access-ml-pipeline", func(profile *kubeflowv1.Profile) *kubeflowv1alpha1.PodDefault {
		var expirationSeconds = int64(7200)
		return &kubeflowv1alpha1.PodDefault{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "access-ml-pipeline",
				Namespace: profile.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
				},
			},
			Spec: kubeflowv1alpha1.PodDefaultSpec{
				Desc: "Allow access to Kubeflow Pipelines",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"access-ml-pipeline": "true",
					},
				},
				Volumes: []v1.Volume{
					{
						Name: "volume-kf-pipeline-token",
						VolumeSource: v1.VolumeSource{
							Projected: &v1.ProjectedVolumeSource{
								Sources: []v1.VolumeProjection{
									{
										ServiceAccountToken: &v1.ServiceAccountTokenProjection{
											Path:              "token",
											ExpirationSeconds: &expirationSeconds,
											Audience:          "pipelines.kubeflow.org",
										},
									},
								},
							},
						},
					},
				},
				VolumeMounts: []v1.VolumeMount{
					{
						MountPath: "/var/run/secrets/kubeflow/pipelines",
						Name:      "volume-kf-pipeline-token",
						ReadOnly:  true,
					},
				},
				Env: []v1.EnvVar{
					{
						Name:  "KF_PIPELINES_SA_TOKEN_PATH",
						Value: "/var/run/secrets/kubeflow/pipelines/token",
					},
				},
			},
		}
	})
}
