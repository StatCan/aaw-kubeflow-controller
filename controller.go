/*
Copyright 2017 The Kubernetes Authors.

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

package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	v1informers "k8s.io/client-go/informers/core/v1"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	kubeflowv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
	kubeflowv1alpha1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1alpha1"
	clientset "github.com/StatCan/kubeflow-controller/pkg/generated/clientset/versioned"
	kubeflowscheme "github.com/StatCan/kubeflow-controller/pkg/generated/clientset/versioned/scheme"
	informers "github.com/StatCan/kubeflow-controller/pkg/generated/informers/externalversions/kubeflowcontroller/v1"
	v1alpha1informers "github.com/StatCan/kubeflow-controller/pkg/generated/informers/externalversions/kubeflowcontroller/v1alpha1"
	listers "github.com/StatCan/kubeflow-controller/pkg/generated/listers/kubeflowcontroller/v1"
	v1alpha1listers "github.com/StatCan/kubeflow-controller/pkg/generated/listers/kubeflowcontroller/v1alpha1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istio "istio.io/client-go/pkg/clientset/versioned"
	istionetworkingv1alpha3informers "istio.io/client-go/pkg/informers/externalversions/networking/v1alpha3"
	istionetworkingv1alpha3listers "istio.io/client-go/pkg/listers/networking/v1alpha3"
)

const controllerAgentName = "kubeflow-controller"
const pachydermNamespace = "pachyderm"

const (
	// SuccessSynced is used as part of the Event 'reason' when a Profile is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Profile fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Profile"
	// MessageResourceSynced is the message used for an Event fired when a Profile
	// is synced successfully
	MessageResourceSynced = "Profile synced successfully"
)

// Controller is the controller implementation for Profile resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// kubeflowclientset is a clientset for our own API group
	kubeflowclientset clientset.Interface
	// istioclienset is a clientset for the Istio APIs
	istioclientset istio.Interface

	podDefaultsLister    v1alpha1listers.PodDefaultLister
	podDefaultsSynced    cache.InformerSynced
	secretsLister        v1listers.SecretLister
	secretsSynced        cache.InformerSynced
	serviceAccountLister v1listers.ServiceAccountLister
	serviceAccountSynced cache.InformerSynced
	roleBindingLister    rbacv1listers.RoleBindingLister
	roleBindingSynced    cache.InformerSynced
	profilesLister       listers.ProfileLister
	profilesSynced       cache.InformerSynced
	envoyFiltersLister   istionetworkingv1alpha3listers.EnvoyFilterLister
	envoyFiltersSynced   cache.InformerSynced

	dockerConfigJSON []byte

	vaultConfigurer VaultConfigurer

	minio MinIO

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new kubeflow controller
func NewController(
	kubeclientset kubernetes.Interface,
	kubeflowclientset clientset.Interface,
	istioclientset istio.Interface,
	podDefaultInformer v1alpha1informers.PodDefaultInformer,
	secretInformer v1informers.SecretInformer,
	serviceAccountInformer v1informers.ServiceAccountInformer,
	roleBindingInformer rbacv1informers.RoleBindingInformer,
	profileInformer informers.ProfileInformer,
	envoyFiltersInformer istionetworkingv1alpha3informers.EnvoyFilterInformer,
	dockerConfigJSON []byte,
	vaultConfigurer VaultConfigurer,
	minio MinIO) *Controller {

	// Create event broadcaster
	// Add kubeflow-controller types to the default Kubernetes Scheme so Events can be
	// logged for kubeflow-controller types.
	utilruntime.Must(kubeflowscheme.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedv1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:        kubeclientset,
		kubeflowclientset:    kubeflowclientset,
		istioclientset:       istioclientset,
		podDefaultsLister:    podDefaultInformer.Lister(),
		podDefaultsSynced:    podDefaultInformer.Informer().HasSynced,
		secretsLister:        secretInformer.Lister(),
		secretsSynced:        secretInformer.Informer().HasSynced,
		serviceAccountLister: serviceAccountInformer.Lister(),
		serviceAccountSynced: serviceAccountInformer.Informer().HasSynced,
		roleBindingLister:    roleBindingInformer.Lister(),
		roleBindingSynced:    roleBindingInformer.Informer().HasSynced,
		profilesLister:       profileInformer.Lister(),
		profilesSynced:       profileInformer.Informer().HasSynced,
		envoyFiltersLister:   envoyFiltersInformer.Lister(),
		envoyFiltersSynced:   envoyFiltersInformer.Informer().HasSynced,
		dockerConfigJSON:     dockerConfigJSON,
		vaultConfigurer:      vaultConfigurer,
		minio:                minio,
		workqueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Profiles"),
		recorder:             recorder,
	}

	klog.Info("Setting up event handlers")
	// Set up an event handler for when Profile resources change
	profileInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueProfile,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueProfile(new)
		},
	})

	// Set up an event handler for when PodDefault resources change. This
	// handler will lookup the owner of the given PodDefault, and if it is
	// owned by a Profile resource will enqueue that Profile resource for
	// processing. This way, we don't need to implement custom logic for
	// handling PodDefault resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	podDefaultInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPD := new.(*kubeflowv1alpha1.PodDefault)
			oldPD := old.(*kubeflowv1alpha1.PodDefault)
			if newPD.ResourceVersion == oldPD.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	// Set up an event handler for when Secret resources change. This
	// handler will lookup the owner of the given Secret, and if it is
	// owned by a Profile resource will enqueue that Profile resource for
	// processing. This way, we don't need to implement custom logic for
	// handling Secret resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPD := new.(*v1.Secret)
			oldPD := old.(*v1.Secret)
			if newPD.ResourceVersion == oldPD.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	// Set up an event handler for when ServiceAccount resources change. This
	// handler will lookup the owner of the given ServiceAccount, and if it is
	// owned by a Profile resource will enqueue that Profile resource for
	// processing. This way, we don't need to implement custom logic for
	// handling ServiceAccount resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	serviceAccountInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPD := new.(*v1.ServiceAccount)
			oldPD := old.(*v1.ServiceAccount)
			if newPD.ResourceVersion == oldPD.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	// Set up an event handler for when RoleBinding resources change. This
	// handler will lookup the owner of the given RoleBinding, and if it is
	// owned by a Profile resource will enqueue that Profile resource for
	// processing. This way, we don't need to implement custom logic for
	// handling ServiceAccount resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	roleBindingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPD := new.(*rbacv1.RoleBinding)
			oldPD := old.(*rbacv1.RoleBinding)
			if newPD.ResourceVersion == oldPD.ResourceVersion {
				// Periodic resync will send update events for all known RoleBinding.
				// Two different versions of the same RoleBinding will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	// Set up an event handler for when EnvoyFilter resources change. This
	// handler will lookup the owner of the given EnvoyFilter, and if it is
	// owned by a Profile resource will enqueue that Profile resource for
	// processing. This way, we don't need to implement custom logic for
	// handling ServiceAccount resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	envoyFiltersInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPD := new.(*istionetworkingv1alpha3.EnvoyFilter)
			oldPD := old.(*istionetworkingv1alpha3.EnvoyFilter)
			if newPD.ResourceVersion == oldPD.ResourceVersion {
				// Periodic resync will send update events for all known EnvoyFilter.
				// Two different versions of the same EnvoyFilter will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting Profile controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.podDefaultsSynced, c.secretsSynced, c.profilesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process Profile resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Profile resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Profile resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Get the Profile resource with this namespace/name
	profile, err := c.profilesLister.Get(key)
	if err != nil {
		// The Profile resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("profile '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// Create an array to track all of the PodDefaults managed by this controller.
	podDefaults := make([]*kubeflowv1alpha1.PodDefault, 0)

	// Create PodDefaults
	for podDefaultName, newPodDefault := range PodDefaults {
		if podDefaultName == "" {
			// We choose to absorb the error here as the worker would requeue the
			// resource otherwise. Instead, the next time the resource is updated
			// the resource will be queued again.
			utilruntime.HandleError(fmt.Errorf("%s: PodDefault name must be specified", key))
			return nil
		}

		// Get the PodDefault with the name specified in Profile.spec
		podDefault, err := c.podDefaultsLister.PodDefaults(profile.Name).Get(podDefaultName)
		// If the resource doesn't exist, we'll create it
		if errors.IsNotFound(err) {
			podDefault, err = c.kubeflowclientset.KubeflowV1alpha1().PodDefaults(profile.Name).Create(context.TODO(), newPodDefault(profile), metav1.CreateOptions{})
		}

		// If an error occurs during Get/Create, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// If the PodDefault is not controlled by this Profile resource, we should log
		// a warning to the event recorder and return error msg.
		if !metav1.IsControlledBy(podDefault, profile) {
			msg := fmt.Sprintf(MessageResourceExists, podDefault.Name)
			c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
			return fmt.Errorf(msg)
		}

		// Check against the expected PodDefault to see if the spec has changed.
		expectedPodDefault := newPodDefault(profile)
		if !reflect.DeepEqual(podDefault.Spec, expectedPodDefault.Spec) {
			// Maintain the original meta information
			expectedPodDefault.ObjectMeta = podDefault.ObjectMeta

			klog.V(4).Infof("Profile %s PodDefault %s out of sync", profile.Name, podDefaultName)
			podDefault, err = c.kubeflowclientset.KubeflowV1alpha1().PodDefaults(profile.Name).Update(context.TODO(), expectedPodDefault, metav1.UpdateOptions{})
		}

		// If an error occurs during Update, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// Track PodDefaults associated with the profile.
		podDefaults = append(podDefaults, podDefault)
	}

	// Add a secret into the namespace for the imagePullSecrets
	var secret *v1.Secret
	var serviceAccount *v1.ServiceAccount
	secretName := "image-pull-secret"
	serviceAccountName := "default-editor"

	if len(c.dockerConfigJSON) > 0 {

		// Get the PodDefault with the name specified in Profile.spec
		secret, err = c.secretsLister.Secrets(profile.Name).Get(secretName)
		// If the resource doesn't exist, we'll create it
		if errors.IsNotFound(err) {
			secret, err = c.kubeclientset.CoreV1().Secrets(profile.Name).Create(context.TODO(), newImagePullSecret(profile, c.dockerConfigJSON), metav1.CreateOptions{})
		}

		// If an error occurs during Get/Create, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// If the Deployment is not controlled by this Profile resource, we should log
		// a warning to the event recorder and return error msg.
		if !metav1.IsControlledBy(secret, profile) {
			msg := fmt.Sprintf(MessageResourceExists, secret.Name)
			c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
			return fmt.Errorf(msg)
		}

		// TODO: LOGIC TO COMPARE THE PODDEFAULTS AGAINST EXPECTED STATE.
		//
		// If this number of the replicas on the Profile resource is specified, and the
		// number does not equal the current desired replicas on the Deployment, we
		// should update the Deployment resource.
		// if profile.Spec.Replicas != nil && *profile.Spec.Replicas != *deployment.Spec.Replicas {
		// 	klog.V(4).Infof("Profile %s replicas: %d, deployment replicas: %d", name, *profile.Spec.Replicas, *deployment.Spec.Replicas)
		// 	podDefault, err = c.kubeclientset.AppsV1().Deployments(profile.Namespace).Update(context.TODO(), newPodDefault(profile), metav1.UpdateOptions{})
		// }

		// If an error occurs during Update, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}
	}

	// Get the PodDefault with the name specified in Profile.spec
	serviceAccount, err = c.serviceAccountLister.ServiceAccounts(profile.Name).Get(serviceAccountName)
	// If the resource doesn't exist, exit. We'll loop around and try again,
	// it's likely the Kubeflow profile-controller hasn't created it yet.
	if errors.IsNotFound(err) {
		return err
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	found := -1
	for indx, pullSecret := range serviceAccount.ImagePullSecrets {
		if pullSecret.Name == secretName {
			found = indx
			break
		}
	}

	if found < 0 && len(c.dockerConfigJSON) > 0 {
		// Let's add the imagePullSecret to the ServiceAccount
		serviceAccountCopy := serviceAccount.DeepCopy()
		serviceAccountCopy.ImagePullSecrets = append(serviceAccountCopy.ImagePullSecrets, v1.LocalObjectReference{
			Name: secretName,
		})

		serviceAccount, err = c.kubeclientset.CoreV1().ServiceAccounts(profile.Name).Update(context.TODO(), serviceAccountCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else if found >= 0 && len(c.dockerConfigJSON) == 0 {
		// Let's remove it
		serviceAccountCopy := serviceAccount.DeepCopy()
		serviceAccountCopy.ImagePullSecrets = append(serviceAccountCopy.ImagePullSecrets[:found], serviceAccountCopy.ImagePullSecrets[found+1:]...)

		serviceAccount, err = c.kubeclientset.CoreV1().ServiceAccounts(profile.Name).Update(context.TODO(), serviceAccountCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	// Configure pachyderm role binding
	err = c.doPachydermRoleBinding(profile)

	if err != nil {
		return err
	}

	// Configure seldon role binding
	err = c.doSeldonRoleBinding(profile)

	if err != nil {
		return err
	}

	// Configure argo role binding
	err = c.doArgoRoleBinding(profile)

	if err != nil {
		return err
	}

	// Configure pipelines-header EnvoyFilter
	err = c.doPipelinesIstioEnvoyFilter(profile)

	if err != nil {
		return err
	}

	// Configure vault
	//Get users that have access to the namespace
	roleBindings, err := c.roleBindingLister.RoleBindings(profile.Name).List(labels.Nothing())
	if err != nil {
		return err
	}

	users := make([]string, 0)
	for _, currentRoleBinding := range roleBindings {
		if currentRoleBinding.RoleRef.Name == "kubeflow-edit" {
			for _, subject := range currentRoleBinding.Subjects {
				if subject.Kind == "User" {
					users = append(users, subject.Name)
				}
			}
		}
	}

	err = c.vaultConfigurer.ConfigVaultForProfile(profile.Name, profile.Spec.Owner.Name, users)

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	// Configure MinIO
	// Autocreate MinIO buckets for the user
	if err = c.minio.CreateBucketsForProfile(profile.Name); err != nil {
		return err
	}

	// Finally, we update the status block of the Profile resource to reflect the
	// current state of the world
	err = c.updateProfileStatus(profile, podDefaults, secret, serviceAccount)
	if err != nil {
		return err
	}

	c.recorder.Event(profile, v1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (c *Controller) updateProfileStatus(profile *kubeflowv1.Profile, podDefaults []*kubeflowv1alpha1.PodDefault, secret *v1.Secret, serviceAccount *v1.ServiceAccount) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	profileCopy := profile.DeepCopy()
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the Profile resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	_, err := c.kubeflowclientset.KubeflowV1().Profiles().Update(context.TODO(), profileCopy, metav1.UpdateOptions{})
	return err
}

// enqueueProfile takes a Profile resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Profile.
func (c *Controller) enqueueProfile(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the Profile resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that Profile resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		klog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	klog.V(4).Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a Profile, we should not do anything more
		// with it.
		if ownerRef.Kind != "Profile" {
			return
		}

		profile, err := c.profilesLister.Get(ownerRef.Name)
		if err != nil {
			klog.V(4).Infof("ignoring orphaned object '%s' of profile '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		c.enqueueProfile(profile)
		return
	}
}

func newImagePullSecret(profile *kubeflowv1.Profile, dockerConfigJSON []byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-pull-secret",
			Namespace: profile.Name,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
			},
		},
		Type: v1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigJSON,
		},
	}
}

func (c *Controller) doPachydermRoleBinding(profile *kubeflowv1.Profile) error {
	roleBindingName := fmt.Sprintf("profile-%s", profile.Name)

	// Get the PodDefault with the name specified in Profile.spec
	roleBinding, err := c.roleBindingLister.RoleBindings(pachydermNamespace).Get(roleBindingName)
	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		roleBinding, err = c.kubeclientset.RbacV1().RoleBindings(pachydermNamespace).Create(context.TODO(), newPachydermRoleBinding(profile, roleBindingName), metav1.CreateOptions{})
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	// If the Deployment is not controlled by this Profile resource, we should log
	// a warning to the event recorder and return error msg.
	if !metav1.IsControlledBy(roleBinding, profile) {
		msg := fmt.Sprintf(MessageResourceExists, roleBinding.Name)
		c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	// TODO: LOGIC TO COMPARE THE PODDEFAULTS AGAINST EXPECTED STATE.
	//
	// If this number of the replicas on the Profile resource is specified, and the
	// number does not equal the current desired replicas on the Deployment, we
	// should update the Deployment resource.
	// if profile.Spec.Replicas != nil && *profile.Spec.Replicas != *deployment.Spec.Replicas {
	// 	klog.V(4).Infof("Profile %s replicas: %d, deployment replicas: %d", name, *profile.Spec.Replicas, *deployment.Spec.Replicas)
	// 	podDefault, err = c.kubeclientset.AppsV1().Deployments(profile.Namespace).Update(context.TODO(), newPodDefault(profile), metav1.UpdateOptions{})
	// }

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	return nil
}

func newPachydermRoleBinding(profile *kubeflowv1.Profile, name string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: pachydermNamespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "pachyderm-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "default-editor",
				Namespace: profile.Name,
			},
		},
	}
}

func (c *Controller) doSeldonRoleBinding(profile *kubeflowv1.Profile) error {
	roleBindingName := "seldon-user"

	// Get the PodDefault with the name specified in Profile.spec
	roleBinding, err := c.roleBindingLister.RoleBindings(profile.Name).Get(roleBindingName)
	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		roleBinding, err = c.kubeclientset.RbacV1().RoleBindings(profile.Name).Create(context.TODO(), newSeldonRoleBinding(profile, roleBindingName), metav1.CreateOptions{})
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	// If the Deployment is not controlled by this Profile resource, we should log
	// a warning to the event recorder and return error msg.
	if !metav1.IsControlledBy(roleBinding, profile) {
		msg := fmt.Sprintf(MessageResourceExists, roleBinding.Name)
		c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	// TODO: LOGIC TO COMPARE THE PODDEFAULTS AGAINST EXPECTED STATE.
	//
	// If this number of the replicas on the Profile resource is specified, and the
	// number does not equal the current desired replicas on the Deployment, we
	// should update the Deployment resource.
	// if profile.Spec.Replicas != nil && *profile.Spec.Replicas != *deployment.Spec.Replicas {
	// 	klog.V(4).Infof("Profile %s replicas: %d, deployment replicas: %d", name, *profile.Spec.Replicas, *deployment.Spec.Replicas)
	// 	podDefault, err = c.kubeclientset.AppsV1().Deployments(profile.Namespace).Update(context.TODO(), newPodDefault(profile), metav1.UpdateOptions{})
	// }

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	return nil
}

func newSeldonRoleBinding(profile *kubeflowv1.Profile, name string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: profile.Name,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "seldon-user",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "default-editor",
				Namespace: profile.Name,
			},
			profile.Spec.Owner,
		},
	}
}

func (c *Controller) doArgoRoleBinding(profile *kubeflowv1.Profile) error {
	roleBindingName := "default-editor-argo"

	// Get the PodDefault with the name specified in Profile.spec
	roleBinding, err := c.roleBindingLister.RoleBindings(profile.Name).Get(roleBindingName)
	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		roleBinding, err = c.kubeclientset.RbacV1().RoleBindings(profile.Name).Create(context.TODO(), newArgoRoleBinding(profile, roleBindingName), metav1.CreateOptions{})
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	// If the Deployment is not controlled by this Profile resource, we should log
	// a warning to the event recorder and return error msg.
	if !metav1.IsControlledBy(roleBinding, profile) {
		msg := fmt.Sprintf(MessageResourceExists, roleBinding.Name)
		c.recorder.Event(profile, v1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	// TODO: LOGIC TO COMPARE THE PODDEFAULTS AGAINST EXPECTED STATE.
	//
	// If this number of the replicas on the Profile resource is specified, and the
	// number does not equal the current desired replicas on the Deployment, we
	// should update the Deployment resource.
	// if profile.Spec.Replicas != nil && *profile.Spec.Replicas != *deployment.Spec.Replicas {
	// 	klog.V(4).Infof("Profile %s replicas: %d, deployment replicas: %d", name, *profile.Spec.Replicas, *deployment.Spec.Replicas)
	// 	podDefault, err = c.kubeclientset.AppsV1().Deployments(profile.Namespace).Update(context.TODO(), newPodDefault(profile), metav1.UpdateOptions{})
	// }

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	return nil
}

func newArgoRoleBinding(profile *kubeflowv1.Profile, name string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: profile.Name,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(profile, kubeflowv1.SchemeGroupVersion.WithKind("Profile")),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "argo",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "default-editor",
				Namespace: profile.Name,
			},
			profile.Spec.Owner,
		},
	}
}
