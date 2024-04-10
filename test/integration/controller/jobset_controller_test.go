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
	"sort"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/collections"
	"sigs.k8s.io/jobset/pkg/util/testing"
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
	// update contains the mutations to perform on the jobs/jobset and the
	// checks to perform afterwards.
	type update struct {
		jobSetUpdateFn       func(*jobset.JobSet)
		jobUpdateFn          func(*batchv1.JobList)
		checkJobCreation     func(*jobset.JobSet)
		checkJobSetState     func(*jobset.JobSet)
		checkJobSetCondition func(context.Context, client.Client, *jobset.JobSet, time.Duration)
	}

	type testCase struct {
		makeJobSet        func(*corev1.Namespace) *testing.JobSetWrapper
		skipCreationCheck bool
		updates           []*update
	}

	nodeSelectors := map[string]map[string]string{
		"replicated-job-a": {"node-selector-test-a": "node-selector-test-a"},
		"replicated-job-b": {"node-selector-test-b": "node-selector-test-b"},
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
			for _, up := range tc.updates {
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						ginkgo.By("completing a job")
						completeJob(&jobList.Items[1])
					},
					checkJobSetCondition: testutil.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("jobset with no failure policy should fail if any jobs fail", &testCase{
			makeJobSet: testJobSet,
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
		ginkgo.Entry("job succeeds after one failure", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					FailurePolicy(&jobset.FailurePolicy{
						MaxRestarts: 1,
					})
			},
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
				{
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are suspended")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, true).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetSuspended,
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						updateJobSetNodeSelectors(js, nodeSelectors)
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
						gomega.Eventually(matchJobsNodeSelectors, timeout, interval).WithArguments(js, nodeSelectors).Should(gomega.Equal(true))
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
			updates: []*update{
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
								Name:  "replicated-job-a",
								Ready: 1,
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
								Suspended: 0,
							},
							{
								Name:      "replicated-job-a",
								Ready:     1,
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
			updates: []*update{
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
								Name:  "replicated-job-a",
								Ready: 1,
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
								Name:  "replicated-job-a",
								Ready: 1,
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
								Name:  "replicated-job-b",
								Ready: 3,
							},
							{
								Name:  "replicated-job-a",
								Ready: 1,
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
			updates: []*update{
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
								Name:  "replicated-job-a",
								Ready: 1,
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
								Name:  "replicated-job-b",
								Ready: 3,
							},
							{
								Name:  "replicated-job-a",
								Ready: 1,
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
								Name:  "replicated-job-a",
								Ready: 1,
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
								Name:  "replicated-job-b",
								Ready: 3,
							},
							{
								Name:  "replicated-job-a",
								Ready: 1,
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
}) // end of Describe

func makeAllJobsReady(jl *batchv1.JobList) {
	for _, job := range jl.Items {
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
	updateJobStatus(job, batchv1.JobStatus{
		Conditions: append(job.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		}),
		Succeeded: ptr.Deref(job.Spec.Parallelism, 0),
	})
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
	updateJobStatus(job, batchv1.JobStatus{Ready: job.Spec.Parallelism})
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
			idx := collections.IndexOf(job.Finalizers, metav1.FinalizerDeleteDependents)
			if idx != -1 {
				job.Finalizers = append(job.Finalizers[:idx], job.Finalizers[idx+1:]...)
				if err := k8sClient.Update(ctx, &job); err != nil {
					return false, err
				}
				expectedFinalizers -= 1
			}
		}
		return expectedFinalizers == 0, nil
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

func failJob(job *batchv1.Job) {
	updateJobStatus(job, batchv1.JobStatus{
		Conditions: append(job.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		}),
	})
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

func updateJobSetNodeSelectors(js *jobset.JobSet, nodeSelectors map[string]map[string]string) {
	gomega.Eventually(func() error {
		var jsGet jobset.JobSet
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jsGet); err != nil {
			return err
		}
		for index := range jsGet.Spec.ReplicatedJobs {
			jsGet.Spec.ReplicatedJobs[index].
				Template.Spec.Template.Spec.NodeSelector = nodeSelectors[jsGet.Spec.ReplicatedJobs[index].Name]
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

func matchJobsNodeSelectors(js *jobset.JobSet, nodeSelectors map[string]map[string]string) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Count number of updated jobs
	jobsUpdated := 0
	for _, job := range jobList.Items {
		rjobName, ok := job.Labels[jobset.ReplicatedJobNameKey]
		if !ok {
			return false, fmt.Errorf(fmt.Sprintf("%s job missing ReplicatedJobName label", job.Name))
		}
		if !apiequality.Semantic.DeepEqual(job.Spec.Template.Spec.NodeSelector, nodeSelectors[rjobName]) {
			return false, nil
		}
		jobsUpdated++
	}
	// Calculate expected number of updated jobs
	wantJobsUpdated := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		if _, exists := nodeSelectors[rjob.Name]; exists {
			wantJobsUpdated += int(rjob.Replicas)
		}
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
	}, timeout, interval).Should(gomega.BeComparableTo(expectedStatus))
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
