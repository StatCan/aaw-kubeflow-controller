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
	"flag"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	kubeinformers "k8s.io/client-go/informers"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"

	clientset "k8s.io/kubeflow-controller/pkg/generated/clientset/versioned"
	informers "k8s.io/kubeflow-controller/pkg/generated/informers/externalversions"
	"k8s.io/kubeflow-controller/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string

	imagePullSecret string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// If an image pull secret wasn't provided, try loading from an environment variable.
	if len(imagePullSecret) == 0 {
		imagePullSecret = os.Getenv("IMAGE_PULL_SECRET")
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	kubeflowClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	kubeflowInformerFactory := informers.NewSharedInformerFactory(kubeflowClient, time.Second*30)

	controller := NewController(kubeClient, kubeflowClient,
		kubeflowInformerFactory.Kubeflow().V1alpha1().PodDefaults(),
		kubeInformerFactory.Core().V1().Secrets(),
		kubeInformerFactory.Core().V1().ServiceAccounts(),
		kubeflowInformerFactory.Kubeflow().V1().Profiles(),
		[]byte(imagePullSecret))

	// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	kubeInformerFactory.Start(stopCh)
	kubeflowInformerFactory.Start(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&imagePullSecret, "image-pull-secret", "", "Encoded dockerconfigjson for the image pull secret. Ignored if empty.")

	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}