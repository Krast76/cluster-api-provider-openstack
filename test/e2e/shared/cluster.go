// +build e2e

/*
Copyright 2021 The Kubernetes Authors.

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

package shared

import (
	"context"
	"path/filepath"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	capie2e "sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/bootstrap"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

// createClusterctlLocalRepository generates a clusterctl repository.
// Must always be run after kubetest.NewConfiguration.
func createClusterctlLocalRepository(config *clusterctl.E2EConfig, repositoryFolder string) string {
	createRepositoryInput := clusterctl.CreateRepositoryInput{
		E2EConfig:        config,
		RepositoryFolder: repositoryFolder,
	}

	// Ensuring a CNI file is defined in the config and register a FileTransformation to inject the referenced file as in place of the CNI_RESOURCES envSubst variable.
	Expect(config.Variables).To(HaveKey(capie2e.CNIPath), "Missing %s variable in the config", capie2e.CNIPath)
	cniPath := config.GetVariable(capie2e.CNIPath)
	Expect(cniPath).To(BeAnExistingFile(), "The %s variable should resolve to an existing file", capie2e.CNIPath)
	createRepositoryInput.RegisterClusterResourceSetConfigMapTransformation(cniPath, capie2e.CNIResources)

	clusterctlConfig := clusterctl.CreateRepository(context.TODO(), createRepositoryInput)
	Expect(clusterctlConfig).To(BeAnExistingFile(), "The clusterctl config file does not exists in the local repository %s", repositoryFolder)
	return clusterctlConfig
}

// setupBootstrapCluster installs Cluster API components via clusterctl.
func setupBootstrapCluster(config *clusterctl.E2EConfig, scheme *runtime.Scheme, useExistingCluster bool) (bootstrap.ClusterProvider, framework.ClusterProxy) {
	var clusterProvider bootstrap.ClusterProvider
	kubeconfigPath := ""
	if !useExistingCluster {
		clusterProvider = bootstrap.CreateKindBootstrapClusterAndLoadImages(context.TODO(), bootstrap.CreateKindBootstrapClusterAndLoadImagesInput{
			Name:               config.ManagementClusterName,
			RequiresDockerSock: config.HasDockerProvider(),
			Images:             config.Images,
		})
		Expect(clusterProvider).ToNot(BeNil(), "Failed to create a bootstrap cluster")

		kubeconfigPath = clusterProvider.GetKubeconfigPath()
		Expect(kubeconfigPath).To(BeAnExistingFile(), "Failed to get the kubeconfig file for the bootstrap cluster")
	}

	// Ensure kubeconfigPath already has been defaulted for the verification below
	// If we're not doing it here, it's done inside of framework.NewClusterProxy()
	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	}

	kubeContext := config.GetVariable(KubeContext)
	if kubeContext != "" {
		kubecfg, err := clientcmd.LoadFromFile(kubeconfigPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(kubecfg.CurrentContext).Should(Equal(kubeContext), "current-context of the kubeconfig should be the same as %s (%s)", KubeContext, kubeContext)
	}

	clusterProxy := framework.NewClusterProxy("bootstrap", kubeconfigPath, scheme)
	Expect(clusterProxy).ToNot(BeNil(), "Failed to get a bootstrap cluster proxy")

	return clusterProvider, clusterProxy
}

// initBootstrapCluster uses kind to create a cluster.
func initBootstrapCluster(e2eCtx *E2EContext) {
	clusterctl.InitManagementClusterAndWatchControllerLogs(context.TODO(), clusterctl.InitManagementClusterAndWatchControllerLogsInput{
		ClusterProxy:            e2eCtx.Environment.BootstrapClusterProxy,
		ClusterctlConfigPath:    e2eCtx.Environment.ClusterctlConfigPath,
		InfrastructureProviders: e2eCtx.E2EConfig.InfrastructureProviders(),
		LogFolder:               filepath.Join(e2eCtx.Settings.ArtifactFolder, "clusters", e2eCtx.Environment.BootstrapClusterProxy.GetName()),
	}, e2eCtx.E2EConfig.GetIntervals(e2eCtx.Environment.BootstrapClusterProxy.GetName(), "wait-controllers")...)
}

// tearDown the bootstrap kind cluster.
func tearDown(bootstrapClusterProvider bootstrap.ClusterProvider, bootstrapClusterProxy framework.ClusterProxy) {
	if bootstrapClusterProxy != nil {
		bootstrapClusterProxy.Dispose(context.TODO())
	}
	if bootstrapClusterProvider != nil {
		bootstrapClusterProvider.Dispose(context.TODO())
	}
}