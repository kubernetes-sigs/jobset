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
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "sigs.k8s.io/jobset/api/config/v1alpha1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/features"
	testingutil "sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("RestartJob", ginkgo.Ordered, func() {
	var ns *corev1.Namespace

	ginkgo.BeforeAll(func() {
		util.UpdateJobSetConfigurationAndRestart(ctx, k8sClient, defaultCfg, func(cfg *configv1alpha1.Configuration) {
			cfg.FeatureGates = map[string]bool{string(features.RestartJob): true}
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
		if util.ShouldDumpNamespace() {
			var printer printers.YAMLPrinter

			ginkgo.By(fmt.Sprintf("dumping relevant resources in namespace %s", ns.Name))

			var jobsets jobset.JobSetList
			gomega.Expect(k8sClient.List(ctx, &jobsets, client.InNamespace(ns.Name))).To(gomega.Succeed())
			for _, js := range jobsets.Items {
				gomega.Expect(printer.PrintObj(&js, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}

			var jobs batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobs, client.InNamespace(ns.Name))).To(gomega.Succeed())
			for _, job := range jobs.Items {
				job.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   batchv1.SchemeGroupVersion.Group,
					Version: batchv1.SchemeGroupVersion.Version,
					Kind:    "Job",
				})
				gomega.Expect(printer.PrintObj(&job, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}
		}

		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("JobSet uses RestartJob failure policy action", func() {
		ginkgo.It("should recover by recreating only the failed job and succeeding eventually", func() {
			ginkgo.By("creating jobset with RestartJob failure policy")
			js := restartJobFailurePolicyTestJobSet(ns)

			ginkgo.By("checking that jobset creation succeeds")
			gomega.Eventually(func(g gomega.Gomega) {
				g.Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, js))).To(gomega.Succeed())
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking jobset succeeds")
			util.JobSetCompleted(ctx, k8sClient, js, timeout)

			ginkgo.By("checking the jobset was properly restarted at job level")
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, js)).Should(gomega.Succeed())
			gomega.Expect(js.Status.Restarts).To(gomega.Equal(int32(0)))
			gomega.Expect(js.Status.RestartsCountTowardsMax).To(gomega.Equal(int32(0)))
			gomega.Expect(js.Status.ReplicatedJobsStatus[0].JobRestarts).NotTo(gomega.BeNil())
		})
	})
})

func restartJobFailurePolicyTestJobSet(ns *corev1.Namespace) *jobset.JobSet {
	jsName := "js-restart-job"
	rjobName := "rjob"
	replicas := 2

	jobTemplate := testingutil.MakeJobTemplate("job", ns.Name).
		PodSpec(corev1.PodSpec{
			RestartPolicy: "Never",
			Containers: []corev1.Container{
				{
					Name:    "test-container",
					Image:   "docker.io/library/bash:latest",
					Command: []string{"bash", "-c"},
					Args: []string{
						`if [[ "$JOB_INDEX" == "1" && "$JOB_RESTART_ATTEMPT" == "0" ]]; then echo "Failing intentionally"; exit 1; else echo "Succeeding"; exit 0; fi`,
					},
					Env: []corev1.EnvVar{
						{
							Name: "JOB_INDEX",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: fmt.Sprintf("metadata.annotations['%s']", jobset.JobIndexKey),
								},
							},
						},
						{
							Name: "JOB_RESTART_ATTEMPT",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: fmt.Sprintf("metadata.annotations['%s']", constants.JobRestartAttemptKey),
								},
							},
						},
					},
				},
			},
		}).Obj()

	jobTemplate.Spec.BackoffLimit = ptr.To[int32](0)

	return testingutil.MakeJobSet(jsName, ns.Name).
		FailurePolicy(&jobset.FailurePolicy{
			MaxRestarts: 3,
			Rules: []jobset.FailurePolicyRule{
				{
					Action: jobset.RestartJob,
					OnJobFailureReasons: []string{
						"BackoffLimitExceeded",
					},
				},
			},
		}).
		ReplicatedJob(testingutil.MakeReplicatedJob(rjobName).
			Job(jobTemplate).
			Replicas(int32(replicas)).
			Obj()).
		Obj()
}
