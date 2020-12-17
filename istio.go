package main

import (
	"context"
	"fmt"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeflowv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
	"istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

func (c *Controller) doPipelinesIstioEnvoyFilter(profile *kubeflowv1.Profile) error {
	envoyFilterName := "kubeflow-pipelines"

	envoyFilter, err := c.envoyFiltersLister.EnvoyFilters(profile.Name).Get(envoyFilterName)
	newEnvoyFilter := newPipelinesIstioEnvoyFilter(profile, envoyFilterName)

	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		envoyFilter, err = c.istioclientset.NetworkingV1alpha3().EnvoyFilters(profile.Name).Create(context.TODO(), newEnvoyFilter, metav1.CreateOptions{})
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	// If the Deployment is not controlled by this Profile resource, we should log
	// a warning to the event recorder and return error msg.
	if !metav1.IsControlledBy(envoyFilter, profile) {
		msg := fmt.Sprintf(MessageResourceExists, envoyFilter.Name)
		c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	if !reflect.DeepEqual(envoyFilter.Spec, newEnvoyFilter.Spec) {
		// Update envoyFilter as it is not the same
		envoyFilter.Spec = newEnvoyFilter.Spec
		envoyFilter, err = c.istioclientset.NetworkingV1alpha3().EnvoyFilters(profile.Name).Update(context.TODO(), envoyFilter, metav1.UpdateOptions{})
	}

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	return nil
}

func newPipelinesIstioEnvoyFilter(profile *kubeflowv1.Profile, name string) *istionetworkingv1alpha3.EnvoyFilter {
	return &istionetworkingv1alpha3.EnvoyFilter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: profile.Name,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
			},
		},
		Spec: v1alpha3.EnvoyFilter{
			ConfigPatches: []*v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: v1alpha3.EnvoyFilter_VIRTUAL_HOST,
					Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
						Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
						ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &v1alpha3.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &v1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Name: "ml-pipeline.kubeflow.svc.cluster.local:8888",
									Route: &v1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
										Name: "default",
									},
								},
							},
						},
					},
					Patch: &v1alpha3.EnvoyFilter_Patch{
						Operation: v1alpha3.EnvoyFilter_Patch_MERGE,
						Value: buildPatchStruct(fmt.Sprintf(`
{
	"request_headers_to_add": [
		{
			"append": true,
			"header": {
				"key": "kubeflow-userid",
				"value": "%s"
			}
		}
	]
}
`, profile.Spec.Owner.Name)),
					},
				},
			},
		},
	}
}
