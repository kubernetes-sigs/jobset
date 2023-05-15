// /*
// Copyright 2023 The Kubernetes Authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

// 	http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package webhooktest

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

var _ = ginkgo.Describe("jobset webhook defaulting", func() {

	// Each test runs in a separate namespace.
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		// Create test namespace before each test.
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

		// Wait for namespace to exist before proceeding with test.
		gomega.Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
			if err != nil {
				return false
			}
			return true
		}, timeout, interval).Should(gomega.BeTrue())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(util.DeleteNamespace(ctx, k8sClient, ns)).Should(gomega.Succeed())
	})

	type testCase struct {
		makeJobSet      func(ns *corev1.Namespace) *testing.JobSetWrapper
		defaultsApplied func(*jobset.JobSet) bool
	}

	ginkgo.DescribeTable("defaulting on jobset creation",
		func(tc *testCase) {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			var fetchedJS jobset.JobSet
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &fetchedJS), timeout, interval).Should(gomega.Succeed())

			// Check defaulting.
			gomega.Expect(tc.defaultsApplied(&fetchedJS)).Should(gomega.Equal(true))
		},
		ginkgo.Entry("job.spec.completionMode defaults to indexed if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("completionmode-unset", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("test-job").
						Job(testing.MakeJobTemplate("test-job", ns.Name).
							PodSpec(testing.TestPodSpec).Obj()).
						EnableDNSHostnames(true).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				completionMode := js.Spec.ReplicatedJobs[0].Template.Spec.CompletionMode
				return completionMode != nil && *completionMode == batchv1.IndexedCompletion
			},
		}),
		ginkgo.Entry("job.spec.completionMode unchanged if already set", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("completionmode-nonindexed", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("test-job").
						EnableDNSHostnames(false).
						Job(testing.MakeJobTemplate("test-job", ns.Name).
							CompletionMode(batchv1.NonIndexedCompletion).
							PodSpec(testing.TestPodSpec).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				completionMode := js.Spec.ReplicatedJobs[0].Template.Spec.CompletionMode
				return completionMode != nil && *completionMode == batchv1.NonIndexedCompletion
			},
		}),
		ginkgo.Entry("enableDNSHostnames defaults to true if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("enablednshostnames-unset", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("test-job").
						Job(testing.MakeJobTemplate("test-job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodSpec(testing.TestPodSpec).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return js.Spec.ReplicatedJobs[0].Network != nil && *js.Spec.ReplicatedJobs[0].Network.EnableDNSHostnames
			},
		}),
		ginkgo.Entry("pod restart policy defaults to OnFailure if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("enablednshostnames-unset", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("test-job").
						Job(testing.MakeJobTemplate("test-job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodSpec(corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "busybox:latest",
									},
								},
							}).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.RestartPolicy == corev1.RestartPolicyOnFailure
			},
		}),
	) // end of DescribeTable
}) // end of Describe
