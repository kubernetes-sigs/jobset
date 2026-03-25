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

package customconfigs

import (
	"math"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "sigs.k8s.io/jobset/api/config/v1alpha1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
	testingutil "sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("InPlaceRestart", ginkgo.Ordered, func() {
	var ns *corev1.Namespace

	ginkgo.BeforeAll(func() {
		util.UpdateJobSetConfigurationAndRestart(ctx, k8sClient, defaultCfg, func(cfg *configv1alpha1.Configuration) {
			cfg.FeatureGates = map[string]bool{string(features.InPlaceRestart): true}
		})
	})

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-customconfigs-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("InPlaceRestart feature gate is enabled", func() {
		ginkgo.It("should accept a JobSet with InPlaceRestart strategy", func() {
			ginkgo.By("creating jobset with InPlaceRestart strategy")
			js := inPlaceRestartTestJobSet(ns)

			ginkgo.By("checking that jobset creation succeeds")
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, js))).To(gomega.Succeed())
			}, timeout, interval).Should(gomega.Succeed())
		})
	})
})

func inPlaceRestartTestJobSet(ns *corev1.Namespace) *jobset.JobSet {
	jobTemplate := testingutil.MakeJobTemplate("job", ns.Name).
		PodSpec(corev1.PodSpec{
			RestartPolicy: "Never",
			Containers: []corev1.Container{
				{
					Name:    "test-container",
					Image:   "docker.io/library/bash:latest",
					Command: []string{"bash", "-c", "sleep 5"},
				},
			},
		}).Obj()

	jobTemplate.Spec.BackoffLimit = ptr.To[int32](math.MaxInt32)
	jobTemplate.Spec.PodReplacementPolicy = ptr.To(batchv1.Failed)
	jobTemplate.Spec.Completions = ptr.To[int32](1)
	jobTemplate.Spec.Parallelism = ptr.To[int32](1)

	return testingutil.MakeJobSet("js-in-place-restart", ns.Name).
		FailurePolicy(&jobset.FailurePolicy{
			MaxRestarts:     3,
			RestartStrategy: jobset.InPlaceRestart,
		}).
		ReplicatedJob(testingutil.MakeReplicatedJob("rjob").
			Job(jobTemplate).
			Replicas(1).
			Obj()).
		Obj()
}
