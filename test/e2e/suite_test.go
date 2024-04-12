/*
Copyright 2023 The Kubernetes Authors.
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

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	jobset "sigs.k8s.io/jobset/api/jobset/v1beta1"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
	//+kubebuilder:scaffold:imports
)

const (
	timeout  = 10 * time.Minute
	interval = time.Millisecond * 250
)

var (
	k8sClient client.Client
	ctx       context.Context
)

func TestAPIs(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t,
		"End To End Suite",
	)
}

var _ = ginkgo.BeforeSuite(func() {
	ctx = context.Background()
	cfg := config.GetConfigOrDie()
	gomega.ExpectWithOffset(1, cfg).NotTo(gomega.BeNil())

	err := jobset.AddToScheme(scheme.Scheme)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(k8sClient).NotTo(gomega.BeNil())

	JobSetReadyForTesting(k8sClient)
})

func JobSetReadyForTesting(client client.Client) {
	ginkgo.By("waiting for resources to be ready for testing")
	// To verify that webhooks are ready, let's create a simple jobset.
	js := testutils.MakeJobSet("js", "default").
		ReplicatedJob(testutils.MakeReplicatedJob("rjob").
			Job(testutils.MakeJobTemplate("job", "default").
				PodSpec(testutils.TestPodSpec).Obj()).
			Obj()).Obj()

	// Once the creation succeeds, that means the webhooks are ready
	// and we can begin testing.
	gomega.Eventually(func() error {
		return client.Create(context.Background(), js)
	}, timeout, interval).Should(gomega.Succeed())

	// Delete this jobset before beginning tests.
	gomega.Expect(client.Delete(ctx, js))
	gomega.Eventually(func() error {
		return client.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{})
	}).ShouldNot(gomega.Succeed())
}
