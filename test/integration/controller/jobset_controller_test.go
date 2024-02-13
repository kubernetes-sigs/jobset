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
	"time"

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
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/collections"
	"sigs.k8s.io/jobset/pkg/util/testing"
	testutil "sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 5 * time.Second
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
		checkJobSetState     func(*jobset.JobSet)
		checkJobSetCondition func(context.Context, client.Client, *jobset.JobSet, time.Duration)
	}

	type testCase struct {
		makeJobSet func(*corev1.Namespace) *testing.JobSetWrapper
		updates    []*update
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

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

			// Perform a series of updates to jobset resources and check resulting jobset state after each update.
			for _, up := range tc.updates {
				var jobSet jobset.JobSet
				gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobSet)).To(gomega.Succeed())

				if up.jobSetUpdateFn != nil {
					up.jobSetUpdateFn(&jobSet)
				} else if up.jobUpdateFn != nil {
					// Fetch updated job objects so we always have the latest resource versions to perform mutations on.
					// Ensure we have all expected jobs in our jobList before continuing.
					var jobList batchv1.JobList
					gomega.Eventually(func() (bool, error) {
						if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
							return false, err
						}
						return len(jobList.Items) == testutil.NumExpectedJobs(js), nil
					}, timeout, interval).Should(gomega.BeTrue())

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
		ginkgo.Entry("jobset should succeed after all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			updates: []*update{
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: testutil.JobSetCompleted,
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
					},
					checkJobSetCondition: testutil.JobSetActive,
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
						gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: controllers.GenSubdomain(js), Namespace: js.Namespace}, &svc)).To(gomega.Succeed())
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
	) // end of DescribeTable
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
	}
	replicatedJobsStatuses := map[string]map[string]int32{}
	for _, replicatedJobStatus := range js.Status.ReplicatedJobsStatus {
		replicatedJobsStatuses[replicatedJobStatus.Name] = map[string]int32{
			"ready":     replicatedJobStatus.Ready,
			"succeeded": replicatedJobStatus.Succeeded,
			"failed":    replicatedJobStatus.Failed,
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

func ReadyJob(job *batchv1.Job) {
	updateJobStatus(job, batchv1.JobStatus{
		Ready: job.Spec.Parallelism,
	})
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

// 2 replicated jobs:
// - one with 1 replica
// - one with 3 replicas and DNS hostnames enabled
func testJobSet(ns *corev1.Namespace) *testing.JobSetWrapper {
	jobSetName := "test-js"
	return testing.MakeJobSet(jobSetName, ns.Name).
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
		EnableDNSHostnames(true).
		NetworkSubdomain(jobSetName).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
			Job(testing.MakeJobTemplate("test-job-A", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
			Replicas(1).
			Obj()).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
			Job(testing.MakeJobTemplate("test-job-B", ns.Name).PodSpec(testing.TestPodSpec).CompletionMode(batchv1.IndexedCompletion).Obj()).
			Replicas(3).
			Obj())
}
