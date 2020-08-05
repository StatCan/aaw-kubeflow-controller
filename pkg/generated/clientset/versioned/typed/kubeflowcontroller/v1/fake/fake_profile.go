/*
Copyright 2020 Statistics Canada

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
	kubeflowcontrollerv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
)

// FakeProfiles implements ProfileInterface
type FakeProfiles struct {
	Fake *FakeKubeflowV1
}

var profilesResource = schema.GroupVersionResource{Group: "kubeflow.org", Version: "v1", Resource: "profiles"}

var profilesKind = schema.GroupVersionKind{Group: "kubeflow.org", Version: "v1", Kind: "Profile"}

// Get takes name of the profile, and returns the corresponding profile object, and an error if there is any.
func (c *FakeProfiles) Get(ctx context.Context, name string, options v1.GetOptions) (result *kubeflowcontrollerv1.Profile, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(profilesResource, name), &kubeflowcontrollerv1.Profile{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeflowcontrollerv1.Profile), err
}

// List takes label and field selectors, and returns the list of Profiles that match those selectors.
func (c *FakeProfiles) List(ctx context.Context, opts v1.ListOptions) (result *kubeflowcontrollerv1.ProfileList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(profilesResource, profilesKind, opts), &kubeflowcontrollerv1.ProfileList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &kubeflowcontrollerv1.ProfileList{ListMeta: obj.(*kubeflowcontrollerv1.ProfileList).ListMeta}
	for _, item := range obj.(*kubeflowcontrollerv1.ProfileList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested profiles.
func (c *FakeProfiles) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(profilesResource, opts))
}

// Create takes the representation of a profile and creates it.  Returns the server's representation of the profile, and an error, if there is any.
func (c *FakeProfiles) Create(ctx context.Context, profile *kubeflowcontrollerv1.Profile, opts v1.CreateOptions) (result *kubeflowcontrollerv1.Profile, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(profilesResource, profile), &kubeflowcontrollerv1.Profile{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeflowcontrollerv1.Profile), err
}

// Update takes the representation of a profile and updates it. Returns the server's representation of the profile, and an error, if there is any.
func (c *FakeProfiles) Update(ctx context.Context, profile *kubeflowcontrollerv1.Profile, opts v1.UpdateOptions) (result *kubeflowcontrollerv1.Profile, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(profilesResource, profile), &kubeflowcontrollerv1.Profile{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeflowcontrollerv1.Profile), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeProfiles) UpdateStatus(ctx context.Context, profile *kubeflowcontrollerv1.Profile, opts v1.UpdateOptions) (*kubeflowcontrollerv1.Profile, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(profilesResource, "status", profile), &kubeflowcontrollerv1.Profile{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeflowcontrollerv1.Profile), err
}

// Delete takes name of the profile and deletes it. Returns an error if one occurs.
func (c *FakeProfiles) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(profilesResource, name), &kubeflowcontrollerv1.Profile{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeProfiles) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(profilesResource, listOpts)

	_, err := c.Fake.Invokes(action, &kubeflowcontrollerv1.ProfileList{})
	return err
}

// Patch applies the patch and returns the patched profile.
func (c *FakeProfiles) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *kubeflowcontrollerv1.Profile, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(profilesResource, name, pt, data, subresources...), &kubeflowcontrollerv1.Profile{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeflowcontrollerv1.Profile), err
}
