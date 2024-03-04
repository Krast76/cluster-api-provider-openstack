/*
Copyright 2024 The Kubernetes Authors.

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

package apivalidations

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	testScheme *runtime.Scheme
	ctx        = context.Background()
	mgrCancel  context.CancelFunc
	mgrDone    chan struct{}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "API Validation Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			// NOTE: These are the bare CRDs without conversion webhooks
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{
				filepath.Join("..", "..", "..", "..", "config", "webhook"),
			},
		},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred(), "test environment should start")
	Expect(cfg).NotTo(BeNil(), "test environment should return a configuration")
	DeferCleanup(func() error {
		By("tearing down the test environment")
		return testEnv.Stop()
	})

	testScheme = scheme.Scheme
	Expect(infrav1.AddToScheme(testScheme)).To(Succeed())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// CEL requires Kube 1.25 and above, so check for the minimum server version.
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	serverVersion, err := discoveryClient.ServerVersion()
	Expect(err).ToNot(HaveOccurred())

	Expect(serverVersion.Major).To(Equal("1"))

	minorInt, err := strconv.Atoi(serverVersion.Minor)
	Expect(err).ToNot(HaveOccurred())
	Expect(minorInt).To(BeNumerically(">=", 25), fmt.Sprintf("This test suite requires a Kube API server of at least version 1.25, current version is 1.%s", serverVersion.Minor))

	komega.SetClient(k8sClient)
	komega.SetContext(ctx)

	By("Setting up manager and webhooks")
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	Expect(err).ToNot(HaveOccurred(), "Manager setup should succeed")

	Expect((&infrav1.OpenStackMachineTemplateWebhook{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackMachineTemplate webhook should be registered with manager")
	Expect((&infrav1.OpenStackMachineTemplateList{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackMachineTemplateList webhook should be registered with manager")
	Expect((&infrav1.OpenStackCluster{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackCluster webhook should be registered with manager")
	Expect((&infrav1.OpenStackClusterTemplate{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackClusterTemplate webhook should be registered with manager")
	Expect((&infrav1.OpenStackMachine{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackMachine webhook should be registered with manager")
	Expect((&infrav1.OpenStackMachineList{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackMachineList webhook should be registered with manager")
	Expect((&infrav1.OpenStackClusterList{}).SetupWebhookWithManager(mgr)).To(Succeed(), "OpenStackClusterList webhook should be registered with manager")

	By("Starting manager")
	var mgrCtx context.Context
	mgrDone = make(chan struct{})
	mgrCtx, mgrCancel = context.WithCancel(context.Background())

	go func() {
		defer GinkgoRecover()
		defer close(mgrDone)
		Expect(mgr.Start(mgrCtx)).To(Succeed(), "Manager should start")
	}()
	DeferCleanup(func() {
		By("Tearing down manager")
		mgrCancel()
		Eventually(mgrDone).Should(BeClosed(), "Manager should stop")
	})
})

func createNamespace() *corev1.Namespace {
	By("Creating namespace")
	namespace := corev1.Namespace{}
	namespace.GenerateName = "test-"
	Expect(k8sClient.Create(ctx, &namespace)).To(Succeed(), "Namespace creation should succeed")
	DeferCleanup(func() {
		By("Deleting namespace")
		Expect(k8sClient.Delete(ctx, &namespace, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(Succeed(), "Namespace deletion should succeed")
	})
	By(fmt.Sprintf("Using namespace %s", namespace.Name))
	return &namespace
}
