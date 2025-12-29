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

package controllertest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/pkg/workload"
	testutil "sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

var _ = ginkgo.Describe("JobSet validation", func() {
	// jobSetUpdate contains the mutations to perform on the jobset and the
	// checks to perform afterwards.
	type jobSetUpdate struct {
		fn func(*jobset.JobSet)
	}

	type testCase struct {
		makeJobSet func(*corev1.Namespace) *testing.JobSetWrapper
		updates    []*jobSetUpdate
	}

	ginkgo.DescribeTable("JobSet validation during creation and updates",
		func(tc *testCase) {
			ctx := context.Background()

			// Create test namespace for each entry.
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			defer func() {
				gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
			}()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Eventually(func() error {
				return k8sClient.Create(ctx, js)
			}, timeout, interval).Should(gomega.Succeed())

			// Perform updates to the jobset and verify the validation is working correctly.
			for _, update := range tc.updates {

				// Update jobset if specified.
				if update.fn != nil {
					// Verify a valid jobset update succeeded.
					gomega.Eventually(func() error {
						var jsGet jobset.JobSet
						if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
							return err
						}
						update.fn(&jsGet)
						return k8sClient.Update(ctx, &jsGet)
					}, timeout, interval).Should(gomega.Succeed())
				}
			}
		},
		ginkgo.Entry("setting suspend is allowed", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					fn: func(js *jobset.JobSet) {
						js.Spec.Suspend = ptr.To(true)
					},
				},
			},
		}),
	) // end of DescribeTable
}) // end of Describe

var _ = ginkgo.Describe("JobSet controller", func() {
	// step contains the mutations to perform on the jobs/jobset and the
	// checks to perform afterwards.
	type step struct {
		jobSetUpdateFn       func(*jobset.JobSet)
		jobUpdateFn          func(*batchv1.JobList)
		checkJobCreation     func(*jobset.JobSet)
		checkJobSetState     func(*jobset.JobSet)
		checkJobSetCondition func(context.Context, client.Client, *jobset.JobSet, time.Duration)
	}

	type testCase struct {
		makeJobSet        func(*corev1.Namespace) *testing.JobSetWrapper
		skipCreationCheck bool
		steps             []*step
	}

	var podTemplateUpdates = &updatePodTemplateOpts{
		labels:       map[string]string{"label": "value"},
		annotations:  map[string]string{"annotation": "value"},
		nodeSelector: map[string]string{"node-selector-test-a": "node-selector-test-a"},
		tolerations: []corev1.Toleration{
			{
				Key:      "key",
				Operator: corev1.TolerationOpExists,
			},
		},
	}

	ginkgo.DescribeTable("jobset is created and its jobs go through a series of updates",
		func(tc *testCase) {
			ctx := context.Background()
			// Create test namespace for each entry.
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			defer func() {
				gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
			}()

			// Create JobSet.
			js := tc.makeJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By(fmt.Sprintf("creating jobSet %s/%s", js.Name, js.Namespace))
			gomega.Eventually(func() error {
				return k8sClient.Create(ctx, js)
			}, timeout, interval).Should(gomega.Succeed())

			if !tc.skipCreationCheck {
				ginkgo.By("checking all jobs were created successfully")
				gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))
			}

			// Perform a series of updates to jobset resources and check resulting jobset state after each update.
			for _, up := range tc.steps {
				var jobSet jobset.JobSet
				gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobSet)).To(gomega.Succeed())

				if up.jobSetUpdateFn != nil {
					up.jobSetUpdateFn(&jobSet)
				} else if up.jobUpdateFn != nil {
					if up.checkJobCreation == nil {
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))
					} else {
						up.checkJobCreation(&jobSet)
					}
					// Fetch updated job objects so we always have the latest resource versions to perform mutations on.
					// Ensure we have all expected jobs in our jobList before continuing.
					var jobList batchv1.JobList
					gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())
					// Perform update on jobs.
					up.jobUpdateFn(&jobList)
				}

				// Check jobset state if specified.
				if up.checkJobSetState != nil {
					up.checkJobSetState(&jobSet)
				}

				// Check jobset status if specified.
				if up.checkJobSetCondition != nil {
					up.checkJobSetCondition(ctx, k8sClient, &jobSet, timeout)
				}
			}
		},
		ginkgo.Entry("jobset should successfully create jobs", &testCase{
			makeJobSet: testJobSet,
		}),
		ginkgo.Entry("jobset should succeed after all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Succeeded: 3,
							},
							{
								Name:      "replicated-job-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("jobset should not succeed if any job is not completed", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing all but 1 job")
						for i := 0; i < len(jobList.Items)-1; i++ {
							completeJob(&jobList.Items[i])
						}
						readyJob(&jobList.Items[len(jobList.Items)-1])
					},
					checkJobSetCondition: testutil.JobSetActive,
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Succeeded: 2,
								Ready:     1,
								Active:    1,
							},
							{
								Name:      "replicated-job-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("success policy 'all' with empty replicated jobs list", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAll,
						TargetReplicatedJobs: []string{},
					})
			},
			steps: []*step{
				{
					// Complete all the jobs in one replicated job, then ensure the JobSet is still active.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing all jobs from replicated-job-a")
						for _, job := range testutil.JobsFromReplicatedJob(jobList, "replicated-job-a") {
							completeJob(job)
						}
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					// Now complete the job in the other replicated job selected by the success policy
					// and ensure the jobset completes.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing all jobs from replicated-job-b")
						for _, job := range testutil.JobsFromReplicatedJob(jobList, "replicated-job-b") {
							completeJob(job)
						}
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("success policy 'all' with replicated jobs specified", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAll,
						TargetReplicatedJobs: []string{"replicated-job-b"},
					})
			},
			steps: []*step{
				{
					// Jobset has 2 replicated jobs, but only 1 is selected in the success policy.
					// Complete all the jobs in the other replicated job and ensure the jobset is still active.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing all jobs from different replicated job")
						for _, job := range testutil.JobsFromReplicatedJob(jobList, "replicated-job-a") {
							completeJob(job)
						}
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					// Complete 1 job from the target replicated job and ensure the jobset is still active.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing 1st job in replicated job selected by success policy")
						jobs := testutil.JobsFromReplicatedJob(jobList, "replicated-job-b")
						completeJob(jobs[0])
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					// Now complete the remaining jobs in the replicated job selected by the success policy
					// and ensure the jobset completes.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing remaining jobs in replicated job selected by success policy")
						jobs := testutil.JobsFromReplicatedJob(jobList, "replicated-job-b")
						for i := 1; i < len(jobs); i++ {
							completeJob(jobs[i])
						}
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("success policy 'any' with replicated job specified", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					// If any of the 3 jobs in replicated-job-b succeeds, the jobset is marked completed.
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAny,
						TargetReplicatedJobs: []string{"replicated-job-b"},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing 1 of 3 jobs in replicated-job-b")
						for _, job := range testutil.JobsFromReplicatedJob(jobList, "replicated-job-b") {
							completeJob(job)
							break
						}
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("success policy 'any' without replicated job specified", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAny,
						TargetReplicatedJobs: []string{}, // applies to all replicatedJobs
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing a job")
						completeJob(&jobList.Items[1])
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset with no failure policy should fail if any jobs fail", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("jobset with DNS hostnames enabled should created 1 headless service per job and succeed when all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("succeeds from first run", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("fails from first run, no restarts", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("jobset fails after reaching max restarts", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[1])
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset fails immediately with FailJobSet failure policy action.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:              jobset.FailJobSet,
								OnJobFailureReasons: []string{},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 0)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset does not fail immediately with FailJobSet failure policy action as the failure reason is not matched.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:              jobset.FailJobSet,
								OnJobFailureReasons: []string{batchv1.JobReasonBackoffLimitExceeded},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset does not fail immediately with FailJobSet failure policy action as the failure message is not matched.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:                      jobset.FailJobSet,
								OnJobFailureReasons:         []string{},
								OnJobFailureMessagePatterns: []string{"some message"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy), message: "some critical error with a message"})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset does not fail immediately with FailJobSet failure policy action as the failure message is not matched, even if reason is matched.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:                      jobset.FailJobSet,
								OnJobFailureReasons:         []string{batchv1.JobReasonPodFailurePolicy},
								OnJobFailureMessagePatterns: []string{"some message"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy), message: "some critical error with a message"})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset fails immediately with FailJobSet failure policy action as the failure reason and message are matched.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:                      jobset.FailJobSet,
								OnJobFailureReasons:         []string{batchv1.JobReasonPodFailurePolicy},
								OnJobFailureMessagePatterns: []string{"some message"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy), message: "some message"})
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 0)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset fails immediately with FailJobSet failure policy action as the failure reason and message are matched, even if message string is not exactly equal to pattern string.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:                      jobset.FailJobSet,
								OnJobFailureReasons:         []string{batchv1.JobReasonPodFailurePolicy},
								OnJobFailureMessagePatterns: []string{"some message"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy), message: "this is some message with critical error"})
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 0)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset restarts with RestartJobSet failure policy action.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:              jobset.RestartJobSet,
								OnJobFailureReasons: []string{batchv1.JobReasonPodFailurePolicy},
							},
							{
								Action:              jobset.FailJobSet,
								OnJobFailureReasons: []string{},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobs are restarted individually with Recreate strategy.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts:     1,
						RestartStrategy: jobset.Recreate,
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js)-1)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking 3/4 jobs were recreated")
						gomega.Eventually(testutil.NumJobsByRestartAttempt, timeout, interval).
							WithArguments(ctx, k8sClient, js).
							Should(gomega.Equal(map[int]int{
								0: 1,
								1: testutil.NumExpectedJobs(js) - 1,
							}))
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						removeForegroundDeletionFinalizers(js, 1)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking that all jobs were recreated")
						gomega.Eventually(testutil.NumJobsByRestartAttempt, timeout, interval).
							WithArguments(ctx, k8sClient, js).
							Should(gomega.Equal(map[int]int{
								// All 4 Jobs should exist on attempt index 1.
								1: 4,
							}))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobs are recreated after all Jobs are deleted with BlockingRecreate strategy.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts:     1,
						RestartStrategy: jobset.BlockingRecreate,
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js)-1)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking 0/4 jobs were recreated since one job still needs to be finalized")
						gomega.Eventually(testutil.NumJobsByRestartAttempt, timeout, interval).
							WithArguments(ctx, k8sClient, js).
							Should(gomega.Equal(map[int]int{
								// One Job should should still exist on attempt index 0.
								0: 1,
							}))
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						ginkgo.By("removing foreground deletion finalizer from the last job")
						removeForegroundDeletionFinalizers(js, 1)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking that all jobs were recreated")
						gomega.Eventually(testutil.NumJobsByRestartAttempt, timeout, interval).
							WithArguments(ctx, k8sClient, js).
							Should(gomega.Equal(map[int]int{
								// All 4 Jobs should exist on attempt index 1.
								1: 4,
							}))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] jobset restarts with RestartJobSetAndIgnoreMaxRestarts failure policy action.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:              jobset.RestartJobSetAndIgnoreMaxRestarts,
								OnJobFailureReasons: []string{batchv1.JobReasonPodFailurePolicy},
							},
							{
								Action:              jobset.FailJobSet,
								OnJobFailureReasons: []string{},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 2)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJobWithOptions(&jobList.Items[0], &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 3)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] job fails and the parent replicated job is contained in TargetReplicatedJobs.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.FailJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonFailedIndexes},
								TargetReplicatedJobs: []string{"replicated-job-b"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-b", &failJobOptions{reason: ptr.To(batchv1.JobReasonFailedIndexes)})
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 0)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] job fails and the parent replicated job is not contained in TargetReplicatedJobs.", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.FailJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonBackoffLimitExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-b", &failJobOptions{reason: ptr.To(batchv1.JobReasonBackoffLimitExceeded)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] failure policy rules order verification test 1", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.FailJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonMaxFailedIndexesExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
							{
								Action:               jobset.RestartJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonMaxFailedIndexesExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonMaxFailedIndexesExceeded)})
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 0)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] failure policy rules order verification test 2", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.RestartJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonMaxFailedIndexesExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
							{
								Action:               jobset.FailJobSet,
								OnJobFailureReasons:  []string{batchv1.JobReasonMaxFailedIndexesExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonMaxFailedIndexesExceeded)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 1)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
			},
		}),
		ginkgo.Entry("[failure policy] failure policy rules order verification test 3", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.RestartJobSetAndIgnoreMaxRestarts,
								OnJobFailureReasons:  []string{batchv1.JobReasonMaxFailedIndexesExceeded},
								TargetReplicatedJobs: []string{"replicated-job-a"},
							},
							{
								Action:               jobset.FailJobSet,
								OnJobFailureReasons:  []string{},
								TargetReplicatedJobs: []string{},
							},
						},
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonMaxFailedIndexesExceeded)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 1)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonMaxFailedIndexesExceeded)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 2)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJobWithOptions(jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonMaxFailedIndexesExceeded)})
					},
					checkJobSetCondition: testutil.JobSetActive,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 3)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failFirstMatchingJob(jobList, "replicated-job-b")
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetRestarts(js, 3)
						matchJobSetRestartsCountTowardsMax(js, 0)
					},
				},
			},
		}),
		ginkgo.Entry("job succeeds after one failure", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
					})
			},
			steps: []*step{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						completeJob(&jobList.Items[0])
						failJob(&jobList.Items[1])
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("jobset created in suspended state", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					Suspend(true)
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, true).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetSuspended,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("Check ReplicatedJobStatus for suspend")
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("resume a suspended jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(true)
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, true).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetSuspended,
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						updatePodTemplates(js, podTemplateUpdates)
					},
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("Check ReplicatedJobStatus for suspend")
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, false)
					},
					checkJobSetState: func(js *jobset.JobSet) {
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetResumed,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking jobs have expected node selectors")
						gomega.Eventually(checkPodTemplateUpdates, timeout, interval).WithArguments(js, podTemplateUpdates).Should(gomega.Equal(true))
					},
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Succeeded: 3,
							},
							{
								Name:      "replicated-job-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("suspend a running jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(false)
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are not suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, true)
					},
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, true).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetSuspended,
				},
			},
		}),
		ginkgo.Entry("service deleted", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns)
			},
			steps: []*step{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					// Fetch headless service created for replicated job and delete it.
					jobSetUpdateFn: func(js *jobset.JobSet) {
						var svc corev1.Service
						gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: controllers.GetSubdomain(js), Namespace: js.Namespace}, &svc)).To(gomega.Succeed())
						gomega.Expect(k8sClient.Delete(ctx, &svc)).To(gomega.Succeed())
					},
					// Service should be recreated during reconciliation.
					checkJobSetState: checkExpectedServices,
				},
			},
		}),
		ginkgo.Entry("update replicatedJobsStatuses after all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						gomega.Eventually(func() bool {
							gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, js)).To(gomega.Succeed())
							return checkJobSetReplicatedJobsStatus(js)
						}, timeout, interval).Should(gomega.Equal(true))
					},
				},
			},
		}),
		ginkgo.Entry("jobset replicatedJobsStatuses should create and update", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(false)
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are not suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
				},
				{
					jobUpdateFn: makeAllJobsReady,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						gomega.Eventually(func() bool {
							gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, js)).To(gomega.Succeed())
							return checkJobSetReplicatedJobsStatus(js)
						}, timeout, interval).Should(gomega.Equal(true))
					},
				},
			},
		}),
		ginkgo.Entry("active jobs are deleted after jobset succeeds", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SuccessPolicy(&jobset.SuccessPolicy{
						Operator:             jobset.OperatorAny,
						TargetReplicatedJobs: []string{}, // applies to all replicatedJobs
					})
			},
			steps: []*step{
				// Complete a job, and ensure JobSet completes based on 'any' success policy.
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						completeJob(&jobList.Items[1])
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
				// Remove foreground deletion finalizers.
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// Completed jobs are not marked for deletion if the JobSet is completed,
						// so we expect the number of foreground deletion finalizers to equal
						// total jobs - 1.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js)-1)
					},
				},
				// Ensure remaining active jobs are deleted.
				{
					checkJobSetState: func(js *jobset.JobSet) {
						checkNoActiveJobs(js, 1)
					},
				},
			},
		}),
		ginkgo.Entry("active jobs are deleted after jobset fails", &testCase{
			makeJobSet: testJobSet,
			steps: []*step{
				// Fail a job to trigger jobset failure.
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					checkJobSetCondition: testutil.JobSetFailed,
				},
				// Remove foreground deletion finalizers.
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js)-1)
					},
				},
				// Ensure remaining active jobs are deleted.
				{
					checkJobSetState: func(js *jobset.JobSet) {
						checkNoActiveJobs(js, 1)
					},
				},
			},
		}),
		ginkgo.Entry("jobset using generateName with enableDNSHostnames should have headless service name set to the jobset name", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).SetGenerateName("name-prefix").EnableDNSHostnames(true)
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						gomega.Eventually(func() error {
							return k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &corev1.Service{})
						}, timeout, interval).Should(gomega.Succeed())
					},
				},
			},
		}),
		ginkgo.Entry("startupPolicy with InOrder; suspend should keep jobs suspended", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(true).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					})
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("startupPolicy with AnyOrder; suspend should keep jobs suspended", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(true).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.AnyOrder,
					})
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("startupPolicy with AnyOrder; resume suspended JobSet", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(true).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.AnyOrder,
					})
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, false)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are not suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 0,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 0,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("startupPolicy with InOrder; resume suspended JobSet", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Suspend(true).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					})
			},
			steps: []*step{
				// Ensure replicated job statuses report all child jobs are suspended.
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 1,
							},
						})
					},
				},
				// Resume jobset. Only first replicated job should be unsuspended due to in-order
				// startup policy.
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, false)
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Suspended: 3,
							},
							{
								Name:      "replicated-job-a",
								Suspended: 0,
							},
						})
					},
				},
				// Update first replicatedJob so all its child jobs are ready. This will allow
				// the next replicatedJob to proceed.
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-a")
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "replicated-job-b",
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyNotFinished,
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-b")
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "replicated-job-b",
								Ready:     3,
								Active:    3,
								Suspended: 0,
							},
							{
								Name:      "replicated-job-a",
								Ready:     1,
								Active:    1,
								Suspended: 0,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyComplete,
				},
			},
		}),
		ginkgo.Entry("startupPolicy InOrder; replicated-job-a not ready then replicated-job-b should not run", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					})
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// First update
					// Replicated-Job-A should be created.
					// Startup Policy Condition is set
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyNotFinished,
				},
				{
					// Second update
					// Set Replicated-Job-A to ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-a")
					},
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "replicated-job-b",
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
				{
					// Set replicated-job-b to all active but not ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						activeReplicatedJob(jobList, "replicated-job-b")
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:   "replicated-job-b",
								Active: 3,
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyNotFinished,
				},
				{
					// Set replicated-job-b to all ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-b")
					},
				},
				{
					// Final state
					// all jobs are ready
					// startup policy condition is set to true
					// and number of jobs equals total
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:   "replicated-job-b",
								Ready:  3,
								Active: 3,
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyComplete,
				},
			},
		}),
		ginkgo.Entry("startupPolicy with InOrder; success policy restart; replicated-job-a ready than replicated-job-b should run", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 1}).
					StartupPolicy(&jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					})
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "replicated-job-b",
							},
							{
								Name: "replicated-job-a",
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyNotFinished,
				},
				{
					// Second update
					// Set Replicated-Job-A to ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-a")
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "replicated-job-b",
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
				},
				{
					// Set replicated-job-b to all ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-b")
					},
				},
				{
					// Final state
					// all jobs are ready
					// startup policy condition is set to true
					// and number of jobs equals total
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:   "replicated-job-b",
								Ready:  3,
								Active: 3,
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyComplete,
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						completeJob(&jobList.Items[0])
						failJob(&jobList.Items[1])
					},
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						// For a restart, all jobs will be deleted and recreated, so we expect a
						// foreground deletion finalizer for every job.
						removeForegroundDeletionFinalizers(js, testutil.NumExpectedJobs(js))
					},
				},
				{
					// recreate and redo startup policy
					// Set Replicated-Job-A to ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-a")
					},
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "replicated-job-b",
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyNotFinished,
				},
				{
					// Set replicated-job-b to ready
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "replicated-job-b")
					},
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:   "replicated-job-b",
								Ready:  3,
								Active: 3,
							},
							{
								Name:   "replicated-job-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
					checkJobSetCondition: testutil.JobSetStartupPolicyComplete,
				},
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are recreated")
						gomega.Eventually(checkJobsRecreated, timeout, interval).WithArguments(js, 1).Should(gomega.Equal(true))
					},
				},
			},
		}),
		ginkgo.Entry("jobset with coordinator set should have annotation and label set on all jobs", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).Coordinator(&jobset.Coordinator{
					ReplicatedJob: "replicated-job-a",
					JobIndex:      0,
					PodIndex:      0,
				})
			},
			steps: []*step{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						gomega.Eventually(func() (bool, error) {
							expectedCoordinator := fmt.Sprintf("%s-%s-%d-%d.%s", "test-js", "replicated-job-a", 0, 0, "test-js")
							return checkCoordinator(js, expectedCoordinator)
						}, timeout, interval).Should(gomega.BeTrue())
					},
				},
			},
		}),
		ginkgo.Entry("DependsOn: rjob-b depends on ready status of rjob-a", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-a").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-b").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(3).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-a",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// First check.
					// Replicated-Job-A must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Set the Replicated-Job-A status to ready.
					// Replicated-Job-B depends on ready status of Replicated-Job-A
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-a")
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-b",
							},
							{
								Name:   "rjob-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
				{
					// Second check.
					// Number of Jobs created must be 4.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 4
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
				},
				{
					// Final check.
					// Update the Replicated-Job-B status to ready.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-b")
					},
					// All Jobs must be in the ready status.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:   "rjob-b",
								Ready:  3,
								Active: 3,
							},
							{
								Name:   "rjob-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("DependsOn: rjob-b depends on complete status of rjob-a, and rjob-c depends on ready status of rjob-b", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-a").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-b").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-a",
								Status: jobset.DependencyComplete,
							},
						}).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-c").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(3).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-b",
								Status: jobset.DependencyReady,
							},
						}).
						Obj())
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// First check.
					// Replicated-Job-A must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Set the Replicated-Job-A status to complete.
					// Replicated-Job-B depends on complete status of Replicated-Job-A
					jobUpdateFn: func(jobList *batchv1.JobList) {
						completeJob(&jobList.Items[0])
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name: "rjob-b",
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
				{
					// Second check.
					// Replicated-Job-A and Replicated-Job-B must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 2
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Set the Replicated-Job-B status to ready.
					// Replicated-Job-C depends on ready status of Replicated-Job-B
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-b")
					},
				},
				{
					// Third check.
					// Replicated-Job-A, Replicated-Job-B, and Replicated-Job-C must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 5
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name:   "rjob-b",
								Ready:  1,
								Active: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
				{
					// Final check.
					// Complete all Jobs.
					jobUpdateFn: completeAllJobs,
					// All Jobs must be in the succeeded status.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "rjob-c",
								Succeeded: 3,
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("DependsOn: rjob-c depends on complete status of rjob-a and rjob-b", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-a").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-b").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-c").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(3).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-a",
								Status: jobset.DependencyComplete,
							},
							{
								Name:   "rjob-b",
								Status: jobset.DependencyComplete,
							},
						}).
						Obj())
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// First check.
					// Replicated-Job-A and Replicated-Job-B must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 2
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Set the Replicated-Job-A and Replicated-Job-B status to complete.
					// Replicated-Job-C depends on complete status of Replicated-Job-A and Replicated-Job-B
					jobUpdateFn: func(jobList *batchv1.JobList) {
						completeJob(&jobList.Items[0])
						completeJob(&jobList.Items[1])
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
				{
					// Second check.
					// Replicated-Job-A, Replicated-Job-B, and Replicated-Job-C must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 5
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
				{
					// Final check.
					// Complete all Jobs.
					jobUpdateFn: completeAllJobs,
					// All Jobs must be in the succeeded status.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "rjob-c",
								Succeeded: 3,
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("DependsOn: rjob-c depends on ready status of rjob-a and complete status of rjob-b", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-a").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-b").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-c").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(3).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-a",
								Status: jobset.DependencyReady,
							},
							{
								Name:   "rjob-b",
								Status: jobset.DependencyComplete,
							},
						}).
						Obj())
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// First check.
					// Replicated-Job-A and Replicated-Job-B must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 2
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Set the Replicated-Job-A status to ready and Replicated-Job-B status to complete.
					// Replicated-Job-C depends on ready status of Replicated-Job-A and complete status of Replicated-Job-B
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-a")
						completeJob(&jobList.Items[1])
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:   "rjob-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
				{
					// Second check.
					// Replicated-Job-A, Replicated-Job-B, and Replicated-Job-C must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 5
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-c",
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:   "rjob-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
				{
					// Final check.
					// Complete all Jobs.
					jobUpdateFn: completeAllJobs,
					// All Jobs must be in the succeeded status.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "rjob-c",
								Succeeded: 3,
							},
							{
								Name:      "rjob-b",
								Succeeded: 1,
							},
							{
								Name:      "rjob-a",
								Succeeded: 1,
							},
						})
					},
				},
			},
		}),
		ginkgo.Entry("DependsOn: resume suspended JobSet when rjob-b depends on ready status of rjob-a", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("depends-on", ns.Name).
					SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-a").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(1).
						Obj()).
					ReplicatedJob(testing.MakeReplicatedJob("rjob-b").
						Job(testing.MakeJobTemplate("job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
						Replicas(3).
						DependsOn([]jobset.DependsOn{
							{
								Name:   "rjob-a",
								Status: jobset.DependencyReady,
							},
						}).
						Obj()).
					Suspend(true)
			},
			skipCreationCheck: true,
			steps: []*step{
				{
					// Ensure that Replicated-Job-A is suspended.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-b",
							},
							{
								Name:      "rjob-a",
								Suspended: 1,
							},
						})
					},
				},
				{
					// Resume the JobSet.
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, false)
					},
					// Only the Replicated-Job-A should be unsuspended due to DependsOn order.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name: "rjob-b",
							},
							{
								Name:      "rjob-a",
								Suspended: 0,
							},
						})
					},
				},
				{
					// The Replicated-Job-A must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 1
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Update the Replicated-Job-A to the ready status.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-a")
					},
				},
				{
					// The Replicated-Job-A and Replicated-Job-B must be created.
					checkJobCreation: func(js *jobset.JobSet) {
						expectedStarts := 4
						gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(expectedStarts))
					},
					// Replicated-Job-B must be unsuspended.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "rjob-b",
								Suspended: 0,
							},
							{
								Name:   "rjob-a",
								Ready:  1,
								Active: 1,
							},
						})
					},
				},
				{
					// Update the Replicated-Job-B to the ready status.
					jobUpdateFn: func(jobList *batchv1.JobList) {
						readyReplicatedJob(jobList, "rjob-b")
					},
					// Replicated-Job-A and Replicated-Job-B must have the correct statuses.
					checkJobSetState: func(js *jobset.JobSet) {
						matchJobSetReplicatedStatus(js, []jobset.ReplicatedJobStatus{
							{
								Name:      "rjob-b",
								Ready:     3,
								Active:    3,
								Suspended: 0,
							},
							{
								Name:      "rjob-a",
								Ready:     1,
								Active:    1,
								Suspended: 0,
							},
						})
					},
				},
			},
		}),
	) // end of DescribeTable

	ginkgo.When("A JobSet is managed by another controller", ginkgo.Ordered, func() {
		var (
			ctx context.Context
			ns  *corev1.Namespace
			js  *jobset.JobSet
		)
		ginkgo.BeforeAll(func() {
			ctx = context.Background()
			// Create test namespace for each entry.
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			js = testJobSet(ns).SetGenerateName("name-prefix").ManagedBy("other-controller").Obj()

			ginkgo.By(fmt.Sprintf("creating jobSet %s/%s", js.Name, js.Namespace))
			gomega.Eventually(func() error {
				return k8sClient.Create(ctx, js)
			}, timeout, interval).Should(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should not create any jobs for it, while suspended", func() {
			var jobList batchv1.JobList
			gomega.Consistently(func(g gomega.Gomega) {
				g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
				g.Expect(len(jobList.Items)).To(gomega.BeZero())
			}, timeout, interval).Should(gomega.Succeed())
		})

		ginkgo.It("Should not create any jobs for it, when unsuspended", func() {
			var jobList batchv1.JobList
			ginkgo.By("Unsuspending the JobSet", func() {
				updatedJs := &jobset.JobSet{}

				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), updatedJs)).To(gomega.Succeed())
					updatedJs.Spec.Suspend = ptr.To(false)
					g.Expect(k8sClient.Update(ctx, updatedJs)).To(gomega.Succeed())

				}).Should(gomega.Succeed())
			})

			gomega.Consistently(func(g gomega.Gomega) {
				g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
				g.Expect(len(jobList.Items)).To(gomega.BeZero())
			}, timeout, interval).Should(gomega.Succeed())
		})

		ginkgo.It("Updates to its status are preserved", func() {
			updatedJs := &jobset.JobSet{}
			wantStatus := jobset.JobSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(jobset.JobSetFailed),
						Status:             metav1.ConditionFalse,
						Reason:             "ByTest",
						LastTransitionTime: metav1.Now(),
					},
				},
				Restarts: 1,
				ReplicatedJobsStatus: []jobset.ReplicatedJobStatus{
					{
						Name:      "replicated-job-a",
						Ready:     2,
						Succeeded: 3,
						Failed:    4,
						Active:    5,
						Suspended: 6,
					},
				},
			}

			ginkgo.By("Updating the JobSet status", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), updatedJs)).To(gomega.Succeed())
					updatedJs.Status = wantStatus
					g.Expect(k8sClient.Status().Update(ctx, updatedJs)).To(gomega.Succeed())

				}).Should(gomega.Succeed())
			})

			gomega.Consistently(func(g gomega.Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), updatedJs)).To(gomega.Succeed())
				g.Expect(updatedJs.Status).To(gomega.BeComparableTo(wantStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")))
			}, timeout, interval).Should(gomega.Succeed())
		})
	})

	ginkgo.When("A JobSet is created with TTLSecondsAfterFinished configured and reaches terminal state", func() {
		ginkgo.It("JobSet controller should delete it after configured ttl duration passes", func() {
			// Create test namespace for each entry.
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}

			gomega.Expect(k8sClient.Create(ctx, ns1)).To(gomega.Succeed())
			gomega.Expect(k8sClient.Create(ctx, ns2)).To(gomega.Succeed())

			defer func() {
				gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns1)).To(gomega.Succeed())
				gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns2)).To(gomega.Succeed())
			}()
			// Create JobSet.
			js1 := testJobSet(ns1).TTLSecondsAfterFinished(2).Obj()
			js2 := testJobSet(ns2).Obj()

			// Verify jobsets created successfully.
			ginkgo.By(fmt.Sprintf("creating jobSet %s/%s", js1.Name, js1.Namespace))
			gomega.Expect(k8sClient.Create(ctx, js1)).Should(gomega.Succeed())
			ginkgo.By(fmt.Sprintf("creating jobSet %s/%s", js2.Name, js2.Namespace))
			gomega.Expect(k8sClient.Create(ctx, js2)).Should(gomega.Succeed())

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js1).Should(gomega.Equal(testutil.NumExpectedJobs(js1)))
			gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js2).Should(gomega.Equal(testutil.NumExpectedJobs(js2)))

			// Fetch updated job objects, so we always have the latest resource versions to perform mutations on.
			var jobList batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js1.Namespace))).Should(gomega.Succeed())
			gomega.Expect(len(jobList.Items)).To(gomega.Equal(testutil.NumExpectedJobs(js1)))
			failJob(&jobList.Items[0])
			gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js2.Namespace))).Should(gomega.Succeed())
			gomega.Expect(len(jobList.Items)).To(gomega.Equal(testutil.NumExpectedJobs(js2)))
			completeAllJobs(&jobList)

			// Verify jobset is marked as completed.
			testutil.JobSetFailed(ctx, k8sClient, js1, timeout)
			testutil.JobSetCompleted(ctx, k8sClient, js2, timeout)

			// Verify active jobs have been deleted after ttl has passed.
			testutil.ExpectJobsDeletionTimestamp(ctx, k8sClient, js1, testutil.NumExpectedJobs(js1)-1, timeout)

			// Verify jobset has been deleted after ttl has passed.
			var fresh1, fresh2 jobset.JobSet
			ginkgo.By("checking that ttl after finished controller deletes only the jobset with ttl set after configured seconds pass")
			gomega.Eventually(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(js1), &fresh1); err != nil {
					return false
				}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(js2), &fresh2); err != nil {
					return false
				}
				return !fresh1.DeletionTimestamp.IsZero() && fresh2.DeletionTimestamp.IsZero()
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.When("a JobSet is created with an invalid Job template", func() {
		ginkgo.It("should emit a warning event", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}

			ginkgo.By("creating namespace")
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			defer func() {
				gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
			}()

			ginkgo.By("creating jobset with an invalid job template")
			podSpec := testing.TestPodSpec
			podSpec.Containers[0].Image = ""

			js := testing.MakeJobSet("invalid-jobset", ns.Name).
				ReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
					Job(testing.MakeJobTemplate("test-job-A", ns.Name).PodSpec(podSpec).Obj()).
					Replicas(1).
					Obj()).
				Obj()

			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			ginkgo.By("checking that a warning event is emitted")
			gomega.Eventually(func() (bool, error) {
				var events corev1.EventList
				if err := k8sClient.List(ctx, &events, client.InNamespace(ns.Name), client.MatchingFieldsSelector{
					Selector: fields.AndSelectors(
						fields.OneTermEqualSelector("involvedObject.kind", "JobSet"),
						fields.OneTermEqualSelector("involvedObject.name", js.Name),
						fields.OneTermEqualSelector("reason", "JobCreationFailed"),
					),
				}); err != nil {
					return false, err
				}

				if len(events.Items) < 1 {
					return false, nil
				}

				event := events.Items[0]
				gomega.Expect(event.Type).To(gomega.Equal(corev1.EventTypeWarning))
				return true, nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("checking that the events are accumulated")
			gomega.Eventually(func() (bool, error) {
				var events corev1.EventList
				if err := k8sClient.List(ctx, &events, client.InNamespace(ns.Name), client.MatchingFieldsSelector{
					Selector: fields.AndSelectors(
						fields.OneTermEqualSelector("involvedObject.kind", "JobSet"),
						fields.OneTermEqualSelector("involvedObject.name", js.Name),
						fields.OneTermEqualSelector("reason", "JobCreationFailed"),
					),
				}); err != nil {
					return false, err
				}

				return len(events.Items) == 1 && events.Items[0].Count > 1, nil
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})
}) // end of Describe

func makeAllJobsReady(jl *batchv1.JobList) {
	for _, job := range jl.Items {
		job.Status.Active = *job.Spec.Parallelism
		job.Status.Ready = job.Spec.Parallelism
		gomega.Eventually(k8sClient.Status().Update(ctx, &job), timeout, interval).Should(gomega.Succeed())
	}
}

func checkJobSetReplicatedJobsStatus(js *jobset.JobSet) bool {
	var jobList batchv1.JobList
	gomega.Eventually(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())
	jobsStatuses := map[string]map[string]int32{}
	for _, job := range jobList.Items {
		jobsStatuses[job.Labels[jobset.ReplicatedJobNameKey]] = map[string]int32{
			"ready":     0,
			"succeeded": 0,
			"failed":    0,
			"suspended": 0,
		}
	}
	for _, job := range jobList.Items {
		ready := ptr.Deref(job.Status.Ready, 0)
		// parallelism is always set as it is otherwise defaulted by k8s to 1
		podsCount := *(job.Spec.Parallelism)
		if job.Spec.Completions != nil && *job.Spec.Completions < podsCount {
			podsCount = *job.Spec.Completions
		}

		if isFinished, conditionType := controllers.JobFinished(&job); isFinished && conditionType == batchv1.JobComplete {
			jobsStatuses[job.Labels[jobset.ReplicatedJobNameKey]]["succeeded"]++
			continue
		}

		if isFinished, conditionType := controllers.JobFinished(&job); isFinished && conditionType == batchv1.JobFailed {
			jobsStatuses[job.Labels[jobset.ReplicatedJobNameKey]]["failed"]++
			continue
		}

		if job.Status.Succeeded+ready >= podsCount {
			if job.Labels != nil && job.Labels[jobset.ReplicatedJobNameKey] != "" {
				jobsStatuses[job.Labels[jobset.ReplicatedJobNameKey]]["ready"]++
			}
		}
		if job.Spec.Suspend != nil && *job.Spec.Suspend {
			jobsStatuses[job.Labels[jobset.ReplicatedJobNameKey]]["suspended"]++
		}
	}
	replicatedJobsStatuses := map[string]map[string]int32{}
	for _, replicatedJobStatus := range js.Status.ReplicatedJobsStatus {
		replicatedJobsStatuses[replicatedJobStatus.Name] = map[string]int32{
			"ready":     replicatedJobStatus.Ready,
			"succeeded": replicatedJobStatus.Succeeded,
			"failed":    replicatedJobStatus.Failed,
			"suspended": replicatedJobStatus.Suspended,
		}
	}
	return apiequality.Semantic.DeepEqual(jobsStatuses, replicatedJobsStatuses)
}

func numExpectedServices(js *jobset.JobSet) int {
	// Expect 1 headless service per jobset, if network and hostnames enabled
	expected := 0
	if js.Spec.Network != nil && *js.Spec.Network.EnableDNSHostnames {
		expected = 1
	}
	return expected
}

func completeAllJobs(jobList *batchv1.JobList) {
	ginkgo.By("completing all jobs")
	for _, job := range jobList.Items {
		completeJob(&job)
	}
}

func completeJob(job *batchv1.Job) {
	now := metav1.Now()
	status := batchv1.JobStatus{
		StartTime: job.Status.StartTime,
		Conditions: append(job.Status.Conditions, []batchv1.JobCondition{
			{
				Type:   batchv1.JobSuccessCriteriaMet,
				Status: corev1.ConditionTrue,
			},
			{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			},
		}...),
		Succeeded:      ptr.Deref(job.Spec.Parallelism, 0),
		CompletionTime: job.Status.CompletionTime,
	}
	// Emulate kube-controller-manager job-controller
	// since finished Job has constraints for non-empty startTime.
	if status.StartTime == nil {
		status.StartTime = &now
	}
	if status.CompletionTime == nil {
		status.CompletionTime = &now
	}
	updateJobStatus(job, status)
}

// mark all jobs that match replicatedJobName as ready
func readyReplicatedJob(jobList *batchv1.JobList, replicatedJobName string) {
	for _, job := range jobList.Items {
		replicatedJobNameFromLabel := job.Labels[jobset.ReplicatedJobNameKey]
		if replicatedJobNameFromLabel == replicatedJobName {
			readyJob(&job)
		}
	}
}

func readyJob(job *batchv1.Job) {
	updateJobStatus(job, batchv1.JobStatus{
		Active: *job.Spec.Parallelism,
		Ready:  job.Spec.Parallelism,
	})
}

// removeForegroundDeletionFinalizers will continually fetch the child jobs for a
// given JobSet until it has deleted all of the expected foreground deletion
// finalizers from the jobs.
func removeForegroundDeletionFinalizers(js *jobset.JobSet, expectedFinalizers int) {
	gomega.Eventually(func() (bool, error) {
		// Get fresh job list.
		var jobList batchv1.JobList
		gomega.Eventually(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())

		for _, job := range jobList.Items {
			idx := slices.Index(job.Finalizers, metav1.FinalizerDeleteDependents)
			if idx != -1 {
				job.Finalizers = append(job.Finalizers[:idx], job.Finalizers[idx+1:]...)
				if err := k8sClient.Update(ctx, &job); err != nil {
					return false, err
				}
				expectedFinalizers -= 1
				if expectedFinalizers == 0 {
					return true, nil
				}
			}
		}
		return false, nil
	}, timeout, interval).Should(gomega.Equal(true))
}

// mark all jobs that match replicatedJobName as active
func activeReplicatedJob(jobList *batchv1.JobList, replicatedJobName string) {
	for _, job := range jobList.Items {
		replicatedJobNameFromLabel := job.Labels[jobset.ReplicatedJobNameKey]
		if replicatedJobNameFromLabel == replicatedJobName {
			activeJob(&job)
		}
	}
}

func activeJob(job *batchv1.Job) {
	updateJobStatus(job, batchv1.JobStatus{Active: *job.Spec.Parallelism})
}

func updateJobStatus(job *batchv1.Job, status batchv1.JobStatus) {
	gomega.Eventually(func() error {
		var jobGet batchv1.Job
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &jobGet); err != nil {
			return err
		}
		jobGet.Status = status
		return k8sClient.Status().Update(ctx, &jobGet)
	}, timeout, interval).Should(gomega.Succeed())
}

type failJobOptions struct {
	reason  *string
	message string
}

func failJobWithOptions(job *batchv1.Job, failJobOpts *failJobOptions) {
	if failJobOpts == nil {
		failJobOpts = &failJobOptions{}
	}
	status := batchv1.JobStatus{
		StartTime: job.Status.StartTime,
		Conditions: append(job.Status.Conditions, []batchv1.JobCondition{
			{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
			},
			{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Reason:  ptr.Deref(failJobOpts.reason, ""),
				Message: failJobOpts.message,
			},
		}...),
	}
	// Emulate kube-controller-manager job-controller
	// since finished Job has constraints for non-empty startTime.
	if status.StartTime == nil {
		status.StartTime = ptr.To(metav1.Now())
	}
	updateJobStatus(job, status)
}

func failJob(job *batchv1.Job) {
	failJobWithOptions(job, nil)
}

// failFirstMatchingJobWithOptions fails the first matching job (in terms of index in jobList) that is a child of
// replicatedJobName with extra options. No job is failed if a matching job does not exist.
func failFirstMatchingJobWithOptions(jobList *batchv1.JobList, replicatedJobName string, failJobOpts *failJobOptions) {
	if jobList == nil {
		return
	}
	if failJobOpts == nil {
		failJobOpts = &failJobOptions{}
	}

	for _, job := range jobList.Items {
		parentReplicatedJob := job.Labels[jobset.ReplicatedJobNameKey]
		if parentReplicatedJob == replicatedJobName {
			failJobWithOptions(&job, failJobOpts)
			return
		}
	}
}

// failFirstMatchingJob fails the first matching job (in terms of index in jobList) that is a child of
// replicatedJobName. No job is failed if a matching job does not exist.
func failFirstMatchingJob(jobList *batchv1.JobList, replicatedJobName string) {
	failFirstMatchingJobWithOptions(jobList, replicatedJobName, nil)
}

func suspendJobSet(js *jobset.JobSet, suspend bool) {
	gomega.Eventually(func() error {
		var jsGet jobset.JobSet
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
			return err
		}
		jsGet.Spec.Suspend = ptr.To(suspend)
		return k8sClient.Update(ctx, &jsGet)
	}, timeout, interval).Should(gomega.Succeed())
}

// updatePodTemplateOpts contains pod template values
// which can be mutated on a ReplicatedJob template
// while a JobSet is suspended.
type updatePodTemplateOpts struct {
	labels       map[string]string
	annotations  map[string]string
	nodeSelector map[string]string
	tolerations  []corev1.Toleration
}

func updatePodTemplates(js *jobset.JobSet, opts *updatePodTemplateOpts) {
	gomega.Eventually(func() error {
		var jsGet jobset.JobSet
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
			return err
		}
		for index := range jsGet.Spec.ReplicatedJobs {
			podTemplate := &jsGet.Spec.ReplicatedJobs[index].Template.Spec.Template
			// Update labels.
			podTemplate.Labels = opts.labels

			// Update annotations.
			podTemplate.Annotations = opts.annotations

			// Update node selector.
			podTemplate.Spec.NodeSelector = opts.nodeSelector

			// Update tolerations.
			podTemplate.Spec.Tolerations = opts.tolerations
		}
		return k8sClient.Update(ctx, &jsGet)
	}, timeout, interval).Should(gomega.Succeed())
}

func matchJobsSuspendState(js *jobset.JobSet, suspend bool) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != testutil.NumExpectedJobs(js) {
		return false, nil
	}

	for _, job := range jobList.Items {
		if *job.Spec.Suspend != suspend {
			return false, nil
		}
	}
	return true, nil
}

func checkPodTemplateUpdates(js *jobset.JobSet, podTemplateUpdates *updatePodTemplateOpts) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Count number of updated jobs
	jobsUpdated := 0
	for _, job := range jobList.Items {
		// Check label was added.
		for label, value := range podTemplateUpdates.labels {
			if job.Spec.Template.Labels[label] != value {
				return false, fmt.Errorf("%s != %s", job.Spec.Template.Labels[label], value)
			}
		}

		// Check annotation was added.
		for annotation, value := range podTemplateUpdates.annotations {
			if job.Spec.Template.Annotations[annotation] != value {
				return false, fmt.Errorf("%s != %s", job.Spec.Template.Labels[annotation], value)
			}
		}

		// Check nodeSelector was updated.
		for label, value := range podTemplateUpdates.nodeSelector {
			if job.Spec.Template.Spec.NodeSelector[label] != value {
				return false, fmt.Errorf("%s != %s", job.Spec.Template.Spec.NodeSelector[label], value)
			}
		}

		// Check tolerations were updated.
		for _, toleration := range podTemplateUpdates.tolerations {
			if !slices.Contains(job.Spec.Template.Spec.Tolerations, toleration) {
				return false, fmt.Errorf("missing toleration %v", toleration)
			}
		}

		jobsUpdated++
	}
	// Calculate expected number of updated jobs
	wantJobsUpdated := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		wantJobsUpdated += int(rjob.Replicas)
	}
	return wantJobsUpdated == jobsUpdated, nil
}

func checkJobsRecreated(js *jobset.JobSet, expectedRestarts int) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != testutil.NumExpectedJobs(js) {
		return false, nil
	}
	// Check all the jobs restart counter has been incremented.
	for _, job := range jobList.Items {
		if job.Labels[constants.RestartsKey] != strconv.Itoa(expectedRestarts) {
			return false, nil
		}
	}
	return true, nil
}

// Check one headless service per job was created successfully.
func checkExpectedServices(js *jobset.JobSet) {
	gomega.Eventually(func() (int, error) {
		var svcList corev1.ServiceList
		if err := k8sClient.List(ctx, &svcList, client.InNamespace(js.Namespace)); err != nil {
			return -1, err
		}
		return len(svcList.Items), nil
	}).Should(gomega.Equal(numExpectedServices(js)))
}

// Check that there are no active jobs owned by jobset, and the
// only remaining jobs are the finished ones.
func checkNoActiveJobs(js *jobset.JobSet, numFinishedJobs int) {
	ginkgo.By("checking there are no active jobs")
	gomega.Eventually(func() (bool, error) {
		var jobList batchv1.JobList
		if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
			return false, err
		}
		for _, job := range jobList.Items {
			if jobActive(&job) {
				return false, nil
			}
		}
		return len(jobList.Items) == numFinishedJobs, nil
	}, timeout, interval).Should(gomega.Equal(true))
}

func jobActive(job *batchv1.Job) bool {
	// Jobs marked for deletion using foreground cascading deletion will have deletion timestamp set,
	// but will still exist until dependent objects with ownerReference.blockOwnerDeletion=true set are deleted.
	if job.DeletionTimestamp != nil {
		return false
	}
	if len(job.Status.Conditions) == 0 {
		return true
	}
	active := true
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			active = false
			break
		}
	}
	return active
}

// matchJobSetRestarts checks that the supplied jobset js has expectedCount
// as the value of js.Status.Restarts.
func matchJobSetRestarts(js *jobset.JobSet, expectedCount int32) {
	gomega.Eventually(func() (int32, error) {
		newJs := jobset.JobSet{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &newJs); err != nil {
			return 0, err
		}

		return newJs.Status.Restarts, nil
	}, timeout, interval).Should(gomega.BeComparableTo(expectedCount))
}

// matchJobSetRestartsCountTowardsMax checks that the supplied jobset js has expectedCount
// as the value of js.Status.RestartsCountTowardsMax.
func matchJobSetRestartsCountTowardsMax(js *jobset.JobSet, expectedCount int32) {
	gomega.Eventually(func() (int32, error) {
		newJs := jobset.JobSet{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &newJs); err != nil {
			return 0, err
		}

		return newJs.Status.RestartsCountTowardsMax, nil
	}, timeout, interval).Should(gomega.Equal(expectedCount))
}

func matchJobSetReplicatedStatus(js *jobset.JobSet, expectedStatus []jobset.ReplicatedJobStatus) {
	gomega.Eventually(func() ([]jobset.ReplicatedJobStatus, error) {
		newJs := jobset.JobSet{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &newJs); err != nil {
			return nil, err
		}
		// ReplicatedStatus is a map and we are not guaranteed to have same order from run to run.
		// This sort allows us to compare statuses consistently.
		compareNames := func(i, j int) bool {
			return newJs.Status.ReplicatedJobsStatus[i].Name > newJs.Status.ReplicatedJobsStatus[j].Name
		}
		sort.Slice(newJs.Status.ReplicatedJobsStatus, compareNames)
		return newJs.Status.ReplicatedJobsStatus, nil
	}, timeout, interval).Should(gomega.Equal(expectedStatus))
}

// checkCoordinator verifies that all child Jobs of a JobSet have the label and annotation:
// jobset.sigs.k8s.io/coordinator=<expectedCoordinator>
// Returns boolean value indicating if the check passed or not.
func checkCoordinator(js *jobset.JobSet, expectedCoordinator string) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != testutil.NumExpectedJobs(js) {
		return false, nil
	}
	// Check all the jobs have the coordinator label and annotation.
	for _, job := range jobList.Items {
		if job.Labels[jobset.CoordinatorKey] != expectedCoordinator {
			return false, nil
		}
		if job.Annotations[jobset.CoordinatorKey] != expectedCoordinator {
			return false, nil
		}
	}
	return true, nil
}

// 2 replicated jobs:
// - one with 1 replica
// - one with 3 replicas and DNS hostnames enabled
func testJobSet(ns *corev1.Namespace) *testing.JobSetWrapper {
	jobSetName := "test-js"
	return testing.MakeJobSet(jobSetName, ns.Name).
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
		EnableDNSHostnames(true).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
			Job(testing.MakeJobTemplate("test-job-A", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
			Replicas(1).
			Obj()).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
			Job(testing.MakeJobTemplate("test-job-B", ns.Name).PodSpec(testing.TestPodSpec).CompletionMode(batchv1.IndexedCompletion).Obj()).
			Replicas(3).
			Obj())
}

var _ = ginkgo.Describe("Gang scheduling", func() {
	ginkgo.When("A JobSet is created with JobSetAsGang gang policy", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with 2 replicated jobs (1 replica + 3 replicas = 4 pods total)
			// Each job has parallelism=1 by default
			js = testing.MakeJobSet("gang-as-jobset", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("ps-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with JobSetAsGang gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload resource with a single PodGroup containing all pods", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has proper labels")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))
			gomega.Expect(wl.Labels[jobset.JobSetUIDKey]).To(gomega.Equal(string(js.UID)))

			ginkgo.By("verifying the Workload has a single PodGroup")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(1))

			ginkgo.By("verifying the PodGroup has the correct MinCount (total pods = 2 workers + 1 ps = 3)")
			// workers: 2 replicas * 1 parallelism = 2 pods
			// ps: 1 replica * 1 parallelism = 1 pod
			// Total: 3 pods
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(3)))

			ginkgo.By("verifying the Workload has owner reference to the JobSet")
			gomega.Expect(wl.OwnerReferences).To(gomega.HaveLen(1))
			gomega.Expect(wl.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
			gomega.Expect(wl.OwnerReferences[0].Kind).To(gomega.Equal("JobSet"))
		})
	})

	ginkgo.When("A JobSet is created with JobSetGangPerReplicatedJob gang policy", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with 2 replicated jobs
			js = testing.MakeJobSet("gang-per-rjob", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("ps-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetGangPerReplicatedJob),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with JobSetGangPerReplicatedJob gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload resource with a PodGroup per replicated job", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has proper labels")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))

			ginkgo.By("verifying the Workload has 2 PodGroups (one per replicated job)")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(2))

			ginkgo.By("verifying each PodGroup has the correct name and MinCount")
			podGroupsByName := make(map[string]schedulingv1alpha1.PodGroup)
			for _, pg := range wl.Spec.PodGroups {
				podGroupsByName[pg.Name] = pg
			}

			// workers: 2 replicas * 1 parallelism = 2 pods
			workersPG, found := podGroupsByName["workers"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(workersPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(workersPG.Policy.Gang.MinCount).To(gomega.Equal(int32(2)))

			// ps: 1 replica * 1 parallelism = 1 pod
			psPG, found := podGroupsByName["ps"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(psPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(psPG.Policy.Gang.MinCount).To(gomega.Equal(int32(1)))
		})
	})

	ginkgo.When("A JobSet is created with JobSetWorkloadTemplate gang policy", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with a custom workload template
			js = testing.MakeJobSet("gang-template", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetWorkloadTemplate),
					Workload: &schedulingv1alpha1.Workload{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"custom-label": "custom-value",
							},
							Annotations: map[string]string{
								"custom-annotation": "custom-value",
							},
						},
						Spec: schedulingv1alpha1.WorkloadSpec{
							PodGroups: []schedulingv1alpha1.PodGroup{
								{
									Name: "custom-pod-group",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 5,
										},
									},
								},
							},
						},
					},
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with JobSetWorkloadTemplate gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload resource from the user-provided template", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has custom labels from template")
			gomega.Expect(wl.Labels["custom-label"]).To(gomega.Equal("custom-value"))

			ginkgo.By("verifying the Workload has JobSet labels added")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))
			gomega.Expect(wl.Labels[jobset.JobSetUIDKey]).To(gomega.Equal(string(js.UID)))

			ginkgo.By("verifying the Workload has custom annotations from template")
			gomega.Expect(wl.Annotations["custom-annotation"]).To(gomega.Equal("custom-value"))

			ginkgo.By("verifying the Workload has PodGroups from template")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(1))
			gomega.Expect(wl.Spec.PodGroups[0].Name).To(gomega.Equal("custom-pod-group"))
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(5)))
		})
	})

	ginkgo.When("A JobSet without gang policy is created", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet without gang policy
			js = testing.MakeJobSet("no-gang-policy", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s without gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should not create a Workload resource", func() {
			ginkgo.By("checking that no Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
				return err != nil
			}, timeout, interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.When("A JobSet with gang policy has owner reference set correctly", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with gang policy
			js = testing.MakeJobSet("gang-delete", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should have proper owner reference on Workload for garbage collection", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has owner reference to the JobSet")
			gomega.Expect(wl.OwnerReferences).To(gomega.HaveLen(1))
			gomega.Expect(wl.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
			gomega.Expect(wl.OwnerReferences[0].Kind).To(gomega.Equal("JobSet"))
			gomega.Expect(wl.OwnerReferences[0].APIVersion).To(gomega.Equal("jobset.x-k8s.io/v1alpha2"))
			gomega.Expect(ptr.Deref(wl.OwnerReferences[0].Controller, false)).To(gomega.BeTrue())
			gomega.Expect(ptr.Deref(wl.OwnerReferences[0].BlockOwnerDeletion, false)).To(gomega.BeTrue())
			// Note: envtest does not run the garbage collector, so we can only verify
			// that the owner reference is correctly set, which ensures garbage collection
			// will work in a real Kubernetes cluster.
		})
	})

	ginkgo.When("A JobSet with gang policy has replicated jobs with parallelism > 1", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with parallelism > 1
			js = testing.MakeJobSet("gang-parallelism", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).
						PodSpec(testing.TestPodSpec).
						Parallelism(3).
						Completions(3).
						CompletionMode(batchv1.IndexedCompletion).
						Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with parallelism > 1", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload with MinCount accounting for parallelism", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the PodGroup has the correct MinCount (2 replicas * 3 parallelism = 6 pods)")
			// workers: 2 replicas * 3 parallelism = 6 pods
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(6)))
		})
	})

	// Additional tests for JobSetAsGang policy
	ginkgo.When("A JobSet with JobSetAsGang policy has a single replicated job with high replicas", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with a single replicated job with high replicas
			js = testing.MakeJobSet("gang-single-rjob", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).
						PodSpec(testing.TestPodSpec).
						Parallelism(2).
						Completions(2).
						CompletionMode(batchv1.IndexedCompletion).
						Obj()).
					Replicas(5).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with single replicated job", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload with correct MinCount for single replicated job", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has a single PodGroup")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(1))

			ginkgo.By("verifying the PodGroup has the correct MinCount (5 replicas * 2 parallelism = 10 pods)")
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(10)))
		})
	})

	ginkgo.When("A JobSet with JobSetAsGang policy has many replicated jobs", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with multiple replicated jobs of varying sizes
			js = testing.MakeJobSet("gang-many-rjobs", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("leader").
					Job(testing.MakeJobTemplate("many-leader-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("many-worker-job", ns.Name).
						PodSpec(testing.TestPodSpec).
						Parallelism(2).
						Completions(2).
						CompletionMode(batchv1.IndexedCompletion).
						Obj()).
					Replicas(3).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("many-ps-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("evaluator").
					Job(testing.MakeJobTemplate("many-eval-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with many replicated jobs", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload aggregating all pods from all replicated jobs", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has a single PodGroup (JobSetAsGang aggregates all)")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(1))

			ginkgo.By("verifying the PodGroup has the correct MinCount")
			// leader: 1 replica * 1 parallelism = 1 pod
			// workers: 3 replicas * 2 parallelism = 6 pods
			// ps: 2 replicas * 1 parallelism = 2 pods
			// evaluator: 1 replica * 1 parallelism = 1 pod
			// Total: 10 pods
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(10)))

			ginkgo.By("verifying the PodGroup name matches the JobSet name")
			gomega.Expect(wl.Spec.PodGroups[0].Name).To(gomega.Equal(js.Name))
		})
	})

	// Additional tests for JobSetGangPerReplicatedJob policy
	ginkgo.When("A JobSet with JobSetGangPerReplicatedJob policy has replicated jobs with different parallelism", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with replicated jobs having different parallelism
			js = testing.MakeJobSet("gang-per-rjob-parallelism", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("par-worker-job", ns.Name).
						PodSpec(testing.TestPodSpec).
						Parallelism(4).
						Completions(4).
						CompletionMode(batchv1.IndexedCompletion).
						Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("par-ps-job", ns.Name).
						PodSpec(testing.TestPodSpec).
						Parallelism(2).
						Completions(2).
						CompletionMode(batchv1.IndexedCompletion).
						Obj()).
					Replicas(3).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetGangPerReplicatedJob),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with different parallelism per replicated job", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload with separate PodGroups accounting for parallelism", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has 2 PodGroups")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(2))

			ginkgo.By("verifying each PodGroup has the correct MinCount based on replicas * parallelism")
			podGroupsByName := make(map[string]schedulingv1alpha1.PodGroup)
			for _, pg := range wl.Spec.PodGroups {
				podGroupsByName[pg.Name] = pg
			}

			// workers: 2 replicas * 4 parallelism = 8 pods
			workersPG, found := podGroupsByName["workers"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(workersPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(workersPG.Policy.Gang.MinCount).To(gomega.Equal(int32(8)))

			// ps: 3 replicas * 2 parallelism = 6 pods
			psPG, found := podGroupsByName["ps"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(psPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(psPG.Policy.Gang.MinCount).To(gomega.Equal(int32(6)))
		})
	})

	ginkgo.When("A JobSet with JobSetGangPerReplicatedJob policy has many replicated jobs", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with many replicated jobs
			js = testing.MakeJobSet("gang-per-rjob-many", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("leader").
					Job(testing.MakeJobTemplate("prm-leader-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("prm-worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(4).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("prm-ps-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("evaluator").
					Job(testing.MakeJobTemplate("prm-eval-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetGangPerReplicatedJob),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with many replicated jobs", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload with a PodGroup for each replicated job", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has 4 PodGroups (one per replicated job)")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(4))

			ginkgo.By("verifying each PodGroup has the correct name and MinCount")
			podGroupsByName := make(map[string]schedulingv1alpha1.PodGroup)
			for _, pg := range wl.Spec.PodGroups {
				podGroupsByName[pg.Name] = pg
			}

			// leader: 1 replica
			leaderPG, found := podGroupsByName["leader"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(leaderPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(leaderPG.Policy.Gang.MinCount).To(gomega.Equal(int32(1)))

			// workers: 4 replicas
			workersPG, found := podGroupsByName["workers"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(workersPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(workersPG.Policy.Gang.MinCount).To(gomega.Equal(int32(4)))

			// ps: 2 replicas
			psPG, found := podGroupsByName["ps"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(psPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(psPG.Policy.Gang.MinCount).To(gomega.Equal(int32(2)))

			// evaluator: 1 replica
			evalPG, found := podGroupsByName["evaluator"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(evalPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(evalPG.Policy.Gang.MinCount).To(gomega.Equal(int32(1)))
		})
	})

	// Additional tests for JobSetWorkloadTemplate policy
	ginkgo.When("A JobSet with JobSetWorkloadTemplate policy has multiple custom PodGroups", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with a custom workload template containing multiple PodGroups
			js = testing.MakeJobSet("gang-template-multi", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("multi-worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetWorkloadTemplate),
					Workload: &schedulingv1alpha1.Workload{
						Spec: schedulingv1alpha1.WorkloadSpec{
							PodGroups: []schedulingv1alpha1.PodGroup{
								{
									Name: "critical-pods",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 3,
										},
									},
								},
								{
									Name: "optional-pods",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 1,
										},
									},
								},
							},
						},
					},
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with custom template having multiple PodGroups", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload with multiple PodGroups from the template", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has 2 PodGroups from the template")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(2))

			ginkgo.By("verifying each PodGroup preserves its template configuration")
			podGroupsByName := make(map[string]schedulingv1alpha1.PodGroup)
			for _, pg := range wl.Spec.PodGroups {
				podGroupsByName[pg.Name] = pg
			}

			criticalPG, found := podGroupsByName["critical-pods"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(criticalPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(criticalPG.Policy.Gang.MinCount).To(gomega.Equal(int32(3)))

			optionalPG, found := podGroupsByName["optional-pods"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(optionalPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(optionalPG.Policy.Gang.MinCount).To(gomega.Equal(int32(1)))
		})
	})

	ginkgo.When("A JobSet with JobSetWorkloadTemplate policy preserves all template metadata", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with a custom workload template with extensive metadata
			js = testing.MakeJobSet("gang-template-metadata", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("meta-worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetWorkloadTemplate),
					Workload: &schedulingv1alpha1.Workload{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":         "ml-training",
								"environment": "production",
								"team":        "ml-platform",
							},
							Annotations: map[string]string{
								"description":        "ML training workload",
								"scheduler.priority": "high",
							},
						},
						Spec: schedulingv1alpha1.WorkloadSpec{
							PodGroups: []schedulingv1alpha1.PodGroup{
								{
									Name: "training-pods",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 8,
										},
									},
								},
							},
						},
					},
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with custom template metadata", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should create a Workload preserving all custom labels and annotations", func() {
			ginkgo.By("checking that the Workload is created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has all custom labels from template")
			gomega.Expect(wl.Labels["app"]).To(gomega.Equal("ml-training"))
			gomega.Expect(wl.Labels["environment"]).To(gomega.Equal("production"))
			gomega.Expect(wl.Labels["team"]).To(gomega.Equal("ml-platform"))

			ginkgo.By("verifying the Workload also has JobSet labels added")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))
			gomega.Expect(wl.Labels[jobset.JobSetUIDKey]).To(gomega.Equal(string(js.UID)))

			ginkgo.By("verifying the Workload has all custom annotations from template")
			gomega.Expect(wl.Annotations["description"]).To(gomega.Equal("ML training workload"))
			gomega.Expect(wl.Annotations["scheduler.priority"]).To(gomega.Equal("high"))

			ginkgo.By("verifying the PodGroup configuration is preserved")
			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(1))
			gomega.Expect(wl.Spec.PodGroups[0].Name).To(gomega.Equal("training-pods"))
			gomega.Expect(wl.Spec.PodGroups[0].Policy.Gang.MinCount).To(gomega.Equal(int32(8)))

			ginkgo.By("verifying the Workload has owner reference to the JobSet")
			gomega.Expect(wl.OwnerReferences).To(gomega.HaveLen(1))
			gomega.Expect(wl.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
			gomega.Expect(wl.OwnerReferences[0].Kind).To(gomega.Equal("JobSet"))
		})
	})

	ginkgo.When("A JobSet is created in suspended state with gang policy", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a suspended JobSet with gang policy
			js = testing.MakeJobSet("gang-suspended", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Suspend(true).
				Obj()

			ginkgo.By(fmt.Sprintf("creating suspended JobSet %s/%s with gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should not create a Workload resource while suspended", func() {
			ginkgo.By("checking that no Workload is created while JobSet is suspended")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
				return err != nil
			}, timeout, interval).Should(gomega.BeTrue())
		})

		ginkgo.It("Should create a Workload resource when resumed", func() {
			ginkgo.By("resuming the JobSet")
			gomega.Eventually(func() error {
				var jsGet jobset.JobSet
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
					return err
				}
				jsGet.Spec.Suspend = ptr.To(false)
				return k8sClient.Update(ctx, &jsGet)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking that the Workload is created after resuming")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has proper labels")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))
		})
	})

	ginkgo.When("A running JobSet with gang policy is suspended", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a running JobSet with gang policy
			js = testing.MakeJobSet("gang-suspend-running", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetAsGang),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with gang policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should delete the Workload when JobSet is suspended", func() {
			ginkgo.By("checking that the Workload is initially created")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("suspending the JobSet")
			gomega.Eventually(func() error {
				var jsGet jobset.JobSet
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
					return err
				}
				jsGet.Spec.Suspend = ptr.To(true)
				return k8sClient.Update(ctx, &jsGet)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking that the Workload is deleted after suspending")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
				return err != nil
			}, timeout, interval).Should(gomega.BeTrue())
		})

		ginkgo.It("Should recreate the Workload when JobSet is resumed", func() {
			ginkgo.By("resuming the JobSet")
			gomega.Eventually(func() error {
				var jsGet jobset.JobSet
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
					return err
				}
				jsGet.Spec.Suspend = ptr.To(false)
				return k8sClient.Update(ctx, &jsGet)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking that the Workload is recreated after resuming")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload has proper labels")
			gomega.Expect(wl.Labels[jobset.JobSetNameKey]).To(gomega.Equal(js.Name))
		})
	})

	ginkgo.When("A JobSet with JobSetGangPerReplicatedJob policy is suspended and resumed", ginkgo.Ordered, func() {
		var (
			ns *corev1.Namespace
			js *jobset.JobSet
		)

		ginkgo.BeforeAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "jobset-ns-",
				},
			}
			gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

			// Create a JobSet with JobSetGangPerReplicatedJob policy
			js = testing.MakeJobSet("gang-per-rjob-suspend", ns.Name).
				SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
				EnableDNSHostnames(true).
				ReplicatedJob(testing.MakeReplicatedJob("workers").
					Job(testing.MakeJobTemplate("worker-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(2).
					Obj()).
				ReplicatedJob(testing.MakeReplicatedJob("ps").
					Job(testing.MakeJobTemplate("ps-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
					Replicas(1).
					Obj()).
				GangPolicy(&jobset.GangPolicy{
					Policy: ptr.To(jobset.JobSetGangPerReplicatedJob),
				}).
				Obj()

			ginkgo.By(fmt.Sprintf("creating JobSet %s/%s with JobSetGangPerReplicatedJob policy", js.Namespace, js.Name))
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		})

		ginkgo.AfterAll(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		ginkgo.It("Should handle suspend/resume correctly with multiple PodGroups", func() {
			ginkgo.By("checking that the Workload is initially created with multiple PodGroups")
			workloadName := workload.GenWorkloadName(js)
			var wl schedulingv1alpha1.Workload

			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(2))

			ginkgo.By("suspending the JobSet")
			gomega.Eventually(func() error {
				var jsGet jobset.JobSet
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
					return err
				}
				jsGet.Spec.Suspend = ptr.To(true)
				return k8sClient.Update(ctx, &jsGet)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking that the Workload is deleted after suspending")
			gomega.Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
				return err != nil
			}, timeout, interval).Should(gomega.BeTrue())

			ginkgo.By("resuming the JobSet")
			gomega.Eventually(func() error {
				var jsGet jobset.JobSet
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
					return err
				}
				jsGet.Spec.Suspend = ptr.To(false)
				return k8sClient.Update(ctx, &jsGet)
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking that the Workload is recreated with correct PodGroups")
			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)
			}, timeout, interval).Should(gomega.Succeed())

			gomega.Expect(wl.Spec.PodGroups).To(gomega.HaveLen(2))

			podGroupsByName := make(map[string]schedulingv1alpha1.PodGroup)
			for _, pg := range wl.Spec.PodGroups {
				podGroupsByName[pg.Name] = pg
			}

			workersPG, found := podGroupsByName["workers"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(workersPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(workersPG.Policy.Gang.MinCount).To(gomega.Equal(int32(2)))

			psPG, found := podGroupsByName["ps"]
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(psPG.Policy.Gang).NotTo(gomega.BeNil())
			gomega.Expect(psPG.Policy.Gang.MinCount).To(gomega.Equal(int32(1)))
		})
	})
})
