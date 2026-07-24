/*
Copyright The Kubernetes Authors.
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

// Package schedulingtest holds integration tests for JobSet's
// Workload-Aware Scheduling (WAS) integration, i.e. the JobSet controller's
// creation of scheduling.k8s.io/v1alpha3 Workload and PodGroup objects.
//
// The scheduling.k8s.io/v1alpha3 GenericWorkload API this suite exercises
// has not shipped in any released Kubernetes minor version yet, so the
// standard `setup-envtest` binaries (built from released Kubernetes
// versions) do not register it. This suite therefore requires a
// kube-apiserver binary built from the exact Kubernetes release/pre-release
// tag matching the repo's pinned k8s.io/api version, which is downloaded by
// hack/envtest-scheduling-setup.sh and wired up via the
// `make test-integration-scheduling` target. Running this suite with a
// standard KUBEBUILDER_ASSETS directory will fail in BeforeSuite with
// "group version scheduling.k8s.io/v1alpha3 that has not been registered".
package schedulingtest

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment

	// These global context vars used to pass ctx cancel func to AfterSuite as
	// a workaround for https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	ctx    context.Context
	cancel context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Workload-Aware Scheduling Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	apiServer := &envtest.APIServer{}
	apiServer.Configure().
		Append("feature-gates", "GenericWorkload=true,TopologyAwareWorkloadScheduling=true,DRAWorkloadResourceClaims=true").
		Append("runtime-config", "scheduling.k8s.io/v1alpha3=true")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "components", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		ControlPlane: envtest.ControlPlane{
			APIServer: apiServer,
		},
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = jobset.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = schedulingv1alpha3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	// Set up JobSet reconciler and indexes.
	jobSetReconciler := controllers.NewJobSetReconciler(k8sManager.GetClient(), k8sManager.GetScheme(), k8sManager.GetEventRecorder("jobset"))

	err = controllers.SetupJobSetIndexes(ctx, k8sManager.GetFieldIndexer())
	Expect(err).ToNot(HaveOccurred())

	err = jobSetReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Set up pod reconciler and indexes.
	podReconciler := controllers.NewPodReconciler(k8sManager.GetClient(), k8sManager.GetScheme(), k8sManager.GetEventRecorder("pod"))

	err = controllers.SetupPodIndexes(ctx, k8sManager.GetFieldIndexer())
	Expect(err).ToNot(HaveOccurred())

	err = podReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
