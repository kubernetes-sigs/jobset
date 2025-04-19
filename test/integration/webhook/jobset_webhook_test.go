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
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
		gomega.Eventually(func() error {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
			if err != nil {
				return err
			}
			return nil
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(util.DeleteNamespace(ctx, k8sClient, ns)).Should(gomega.Succeed())
	})

	type testCase struct {
		makeJobSet               func(ns *corev1.Namespace) *testing.JobSetWrapper
		jobSetCreationShouldFail bool
		defaultsApplied          func(*jobset.JobSet) bool
		updateJobSet             func(set *jobset.JobSet)
		updateShouldFail         bool
	}

	ginkgo.DescribeTable("jobset webhook tests",
		func(tc *testCase) {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			if tc.jobSetCreationShouldFail {
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Not(gomega.Succeed()))
				return
			}
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			var fetchedJS jobset.JobSet
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &fetchedJS), timeout, interval).Should(gomega.Succeed())

			if tc.defaultsApplied != nil {
				gomega.Expect(tc.defaultsApplied(&fetchedJS)).Should(gomega.Equal(true))
			}

			if tc.updateJobSet != nil {
				tc.updateJobSet(&fetchedJS)
				// Verify jobset created successfully.
				if tc.updateShouldFail {
					gomega.Expect(k8sClient.Update(ctx, &fetchedJS)).Should(gomega.Not(gomega.Succeed()))
				} else {
					gomega.Expect(k8sClient.Update(ctx, &fetchedJS)).Should(gomega.Succeed())
				}
			}
		},
		ginkgo.Entry("job.spec.completionMode defaults to indexed if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("completionmode-unset", ns.Name).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).Obj()).
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
					EnableDNSHostnames(false).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
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
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodSpec(testing.TestPodSpec).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return js.Spec.Network != nil && *js.Spec.Network.EnableDNSHostnames
			},
		}),
		ginkgo.Entry("publishNotReadyAddresses defaults to true if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("publishnotready-unset", ns.Name).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodSpec(testing.TestPodSpec).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return js.Spec.Network != nil && *js.Spec.Network.PublishNotReadyAddresses
			},
		}),
		ginkgo.Entry("pod restart policy defaults to OnFailure if unset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("enablednshostnames-unset", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
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
		ginkgo.Entry("validate jobSet should not fail on NodeSelectors Update when jobset is suspended", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					Suspend(true).
					EnableDNSHostnames(false).
					PublishNotReadyAddresses(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector = map[string]string{"test": "test"}
			},
			updateShouldFail: false,
		}),
		ginkgo.Entry("validate jobSet should fail on NodeSelectors Update", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector = map[string]string{"test": "test"}
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("validate jobSet immutable for fields over than NodeSelector when jobSet is not suspended", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					PublishNotReadyAddresses(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return true
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Hostname = "test"
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Subdomain = "test"
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("success policy defaults to all", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("success-policy", ns.Name).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				operatorDefaulted := js.Spec.SuccessPolicy != nil && js.Spec.SuccessPolicy.Operator == jobset.OperatorAll
				selectorDefaulted := len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0
				return operatorDefaulted && selectorDefaulted
			},
		}),
		ginkgo.Entry("success policy with invalid replicatedJob name is rejected", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("success-policy", ns.Name).
					EnableDNSHostnames(true).
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAll,
						TargetReplicatedJobs: []string{"does-not-exist"},
					}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			jobSetCreationShouldFail: true,
		}),
		ginkgo.Entry("suspend jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					PublishNotReadyAddresses(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.Suspend = ptr.To(true)
			},
			updateShouldFail: false,
		}),
		ginkgo.Entry("resume jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					PublishNotReadyAddresses(true).
					Suspend(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.Suspend = ptr.To(false)
			},
			updateShouldFail: false,
		}),
		ginkgo.Entry("SuccessPolicy should be immutable", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				newSuccessPolicy := js.Spec.SuccessPolicy.DeepCopy()
				newSuccessPolicy.TargetReplicatedJobs = []string{"test", "test2"}
				js.Spec.SuccessPolicy = newSuccessPolicy
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("FailurePolicy should be immutable", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 3}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.FailurePolicy = &jobset.FailurePolicy{MaxRestarts: 21}
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("FailurePolicy defaults are applied when empty", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					EnableDNSHostnames(true).
					FailurePolicy(&jobset.FailurePolicy{}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				restartStrategy := js.Spec.FailurePolicy.RestartStrategy
				return restartStrategy == jobset.Recreate
			},
		}),
		ginkgo.Entry("validate jobSet immutable for managedBy field", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					Suspend(true).
					EnableDNSHostnames(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.IndexedCompletion).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return true
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ManagedBy = ptr.To("new-manager")
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("updating pod template in suspended jobset is allowed", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("suspended", ns.Name).
					Suspend(true).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodTemplateSpec(corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{"old": "annotation"},
								},
								Spec: testing.TestPodSpec,
							}).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return true
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Annotations["new"] = "annotation"
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.SchedulingGates = []corev1.PodSchedulingGate{
					{
						Name: "example.com/gate",
					},
				}
			},
		}),
		ginkgo.Entry("updating pod template in running jobset is not allowed", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("suspended", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("rjob").
						Job(testing.MakeJobTemplate("job", ns.Name).
							CompletionMode(batchv1.IndexedCompletion).
							PodTemplateSpec(corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{"old": "annotation"},
								},
								Spec: testing.TestPodSpec,
							}).Obj()).
						Obj())
			},
			defaultsApplied: func(js *jobset.JobSet) bool {
				return true
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[0].Template.Spec.Template.Annotations["new"] = "annotation"
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("DependsOn and StartupPolicy: AnyOrder can be set together", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.AnyOrder,
					}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-1").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-2").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-1",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			jobSetCreationShouldFail: false,
		}),
		ginkgo.Entry("DependsOn and StartupPolicy: InOrder can't be set together", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-1").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-2").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-1",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			jobSetCreationShouldFail: true,
		}),
		ginkgo.Entry("DependsOn can't be set for the first ReplicatedJob", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-1").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-1",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			jobSetCreationShouldFail: true,
		}),
		ginkgo.Entry("DependsOn list must contain valid ReplicatedJob status", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-1").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-2").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-1",
								Status: "Failed",
							},
						}).
						Obj())
			},
			jobSetCreationShouldFail: true,
		}),

		ginkgo.Entry("DependsOn must be immutable", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-1").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-2").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							Obj()).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-1",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			updateJobSet: func(js *jobset.JobSet) {
				js.Spec.ReplicatedJobs[1].DependsOn[0] = jobset.DependsOn{Name: "rjob-1", Status: jobset.DependencyComplete}
			},
			updateShouldFail: true,
		}),
		ginkgo.Entry("JobSet can create with no replicated jobs", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name)
			},
			jobSetCreationShouldFail: false,
		}),
	) // end of DescribeTable
}) // end of Describe
