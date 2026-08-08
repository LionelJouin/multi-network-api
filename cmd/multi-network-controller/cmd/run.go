/*
Copyright (c) 2026 The Kubernetes Authors.

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

package cmd

import (
	"context"
	"flag"
	"fmt"
	"time"

	networkKindClientset "github.com/kubernetes-sigs/multi-network-api/pkg/client/clientset/versioned"
	networkKindInformers "github.com/kubernetes-sigs/multi-network-api/pkg/client/informers/externalversions"
	"github.com/kubernetes-sigs/multi-network-api/pkg/controllers/endpointslice"
	"github.com/kubernetes-sigs/multi-network-api/pkg/controllers/networkkind"
	"github.com/kubernetes-sigs/multi-network-api/pkg/podnetworkdevice/v1alpha1"
	"github.com/spf13/cobra"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	defaultInformerResyncPeriod = 30 * time.Second
)

type runOptions struct {
	verbosity int
}

func newCmdRun() *cobra.Command {
	runOpts := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the multi-network controller",
		Long:  `Run the multi-network controller`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOpts.run(cmd.Context())
		},
	}

	cmd.Flags().IntVar(
		&runOpts.verbosity,
		"verbosity",
		0,
		"Log Level.",
	)

	return cmd
}

func (ro *runOptions) run(ctx context.Context) error {
	klog.InitFlags(nil)
	_ = flag.Set("v", fmt.Sprintf("%d", ro.verbosity))
	flag.Parse()

	clientCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to InClusterConfig: %v", err)
	}

	networkKindClient, err := networkKindClientset.NewForConfig(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to NewForConfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to NewForConfig: %v", err)
	}

	apiextensionsClient, err := apiextensionsclientset.NewForConfig(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to NewForConfig: %v", err)
	}

	networkKindInformerFactory := networkKindInformers.NewSharedInformerFactory(networkKindClient, defaultInformerResyncPeriod)
	informerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, defaultInformerResyncPeriod)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(apiextensionsClient, defaultInformerResyncPeriod)

	networkKindController, err := networkkind.NewNetworkKindController(
		networkKindInformerFactory.Multinetwork().V1alpha1().NetworkKinds(),
		apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions(),
		networkKindClient.MultinetworkV1alpha1().NetworkKinds(),
		kubeClient,
	)
	if err != nil {
		return fmt.Errorf("failed to create network kind controller: %v", err)
	}

	podNetworkDeviceCache, err := v1alpha1.NewPodNetworkDeviceCache(
		informerFactory.Core().V1().Pods(),
		informerFactory.Resource().V1().ResourceClaims(),
		informerFactory.Resource().V1().ResourceSlices(),
	)
	if err != nil {
		return fmt.Errorf("failed to create pod network device cache: %v", err)
	}

	endpointSliceController, err := endpointslice.NewEndpointSliceController(
		informerFactory.Discovery().V1().EndpointSlices(),
		informerFactory.Core().V1().Services(),
		podNetworkDeviceCache,
		kubeClient,
	)
	if err != nil {
		return fmt.Errorf("failed to create endpoint slice controller: %v", err)
	}

	networkKindInformerFactory.Start(ctx.Done())
	informerFactory.Start(ctx.Done())
	apiextensionsInformerFactory.Start(ctx.Done())

	networkKindInformerFactory.WaitForCacheSync(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())
	apiextensionsInformerFactory.WaitForCacheSync(ctx.Done())

	go func() {
		err = networkKindController.Run(ctx, 1)
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			klog.FromContext(ctx).Error(err, "failed to run network kind controller")
		}
	}()

	go func() {
		err = podNetworkDeviceCache.Run(ctx, 1)
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			klog.FromContext(ctx).Error(err, "failed to run pod network device cache")
		}
	}()

	go func() {
		err = endpointSliceController.Run(ctx, 1)
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			klog.FromContext(ctx).Error(err, "failed to run endpoint slice controller")
		}
	}()

	<-ctx.Done()

	return nil
}
