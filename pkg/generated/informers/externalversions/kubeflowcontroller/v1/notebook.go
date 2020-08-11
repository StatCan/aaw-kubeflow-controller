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

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	kubeflowcontrollerv1 "github.com/StatCan/kubeflow-controller/pkg/apis/kubeflowcontroller/v1"
	versioned "github.com/StatCan/kubeflow-controller/pkg/generated/clientset/versioned"
	internalinterfaces "github.com/StatCan/kubeflow-controller/pkg/generated/informers/externalversions/internalinterfaces"
	v1 "github.com/StatCan/kubeflow-controller/pkg/generated/listers/kubeflowcontroller/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// NotebookInformer provides access to a shared informer and lister for
// Notebooks.
type NotebookInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.NotebookLister
}

type notebookInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewNotebookInformer constructs a new informer for Notebook type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewNotebookInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredNotebookInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredNotebookInformer constructs a new informer for Notebook type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredNotebookInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubeflowV1().Notebooks(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubeflowV1().Notebooks(namespace).Watch(context.TODO(), options)
			},
		},
		&kubeflowcontrollerv1.Notebook{},
		resyncPeriod,
		indexers,
	)
}

func (f *notebookInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredNotebookInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *notebookInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&kubeflowcontrollerv1.Notebook{}, f.defaultInformer)
}

func (f *notebookInformer) Lister() v1.NotebookLister {
	return v1.NewNotebookLister(f.Informer().GetIndexer())
}
