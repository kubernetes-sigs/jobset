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
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/testing"
	testutil "sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 5 * time.Second
	interval = time.Millisecond * 250
)

var _ = ginkgo.Describe("JobSet validation", func() {
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
		msg := fmt.Sprintf("JobSet Validation Before in Namespace %s", ns.Name)
		ginkgo.By(msg)

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
		msg := fmt.Sprintf("JobSet Validation After Each %s", ns.Name)
		ginkgo.By(msg)

		gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).Should(gomega.Succeed())

	})

	// jobSetUpdate contains the mutations to perform on the jobset and the
	// checks to perform afterwards.
	type jobSetUpdate struct {
		fn            func(*jobset.JobSet)
		shouldSucceed bool
	}

	type testCase struct {
		makeJobSet                  func(*corev1.Namespace) *testing.JobSetWrapper
		jobSetCreationShouldSucceed bool
		updates                     []*jobSetUpdate
	}

	ginkgo.DescribeTable("JobSet validation during creation and updates",
		func(tc *testCase) {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// If we are expected a validation error creating the jobset, end the test early.
			if !tc.jobSetCreationShouldSucceed {
				ginkgo.By("checking that jobset creation fails")
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Not(gomega.Succeed()))
				return
			}

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			// Perform updates to the jobset and verify the validation is working correctly.
			for _, update := range tc.updates {

				// Update jobset if specified.
				if update.fn != nil {
					update.fn(js)

					// If we are expecting a validation error, we can skip the rest of the checks.
					if !update.shouldSucceed {
						gomega.Expect(k8sClient.Update(ctx, js)).Should(gomega.Not(gomega.Succeed()))
						continue
					}
					// Verify a valid jobset update succeeded.
					gomega.Expect(k8sClient.Update(ctx, js)).Should(gomega.Succeed())
				}
			}
		},
		ginkgo.Entry("setting suspend is allowed", &testCase{
			makeJobSet:                  testJobSet,
			jobSetCreationShouldSucceed: true,
			updates: []*jobSetUpdate{
				{
					shouldSucceed: true,
					fn: func(js *jobset.JobSet) {
						js.Spec.Suspend = pointer.Bool(true)
					},
				},
			},
		}),
		ginkgo.Entry("setting suspend is allowed", &testCase{
			makeJobSet:                  testJobSet,
			jobSetCreationShouldSucceed: true,
			updates: []*jobSetUpdate{
				{
					shouldSucceed: true,
					fn: func(js *jobset.JobSet) {
						js.Spec.Suspend = pointer.Bool(true)
					},
				},
			},
		}),
	) // end of DescribeTable
}) // end of Describe

var _ = ginkgo.Describe("JobSet controller", func() {

	// Each test runs in a separate namespace.
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		// Create test namespace before each test.
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "jobset-ns-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

		msg := fmt.Sprintf("BeforeEach with Namespace of %s", ns.Name)
		ginkgo.By(msg)
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
		msg := fmt.Sprintf("AfterEach with Namespace of %s", ns.Name)
		ginkgo.By(msg)

		gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).Should(gomega.Succeed())
		// Wait for namespace to exist before proceeding with test.
	})

	// update contains the mutations to perform on the jobs/jobset and the
	// checks to perform afterwards.
	type update struct {
		jobSetUpdateFn       func(*jobset.JobSet)
		jobUpdateFn          func(*batchv1.JobList)
		checkJobSetState     func(*jobset.JobSet)
		checkJobState        func(*jobset.JobSet)
		checkJobSetCondition func(context.Context, client.Client, *jobset.JobSet, time.Duration)
	}

	type testCase struct {
		makeJobSet               func(*corev1.Namespace) *testing.JobSetWrapper
		jobSetCreationShouldFail bool
		updates                  []*update
	}

	nodeSelectors := map[string]map[string]string{
		"test-js-replicated-job-a": {"node-selector-test-a": "node-selector-test-a"},
		"test-js-replicated-job-b": {"node-selector-test-b": "node-selector-test-b"},
	}

	ginkgo.DescribeTable("jobset is created and its jobs go through a series of updates",
		func(tc *testCase) {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

			// Perform a series of updates to jobset resources and check resulting jobset state after each update.
			for _, up := range tc.updates {

				var jobSet jobset.JobSet
				gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobSet), timeout, interval).Should(gomega.Succeed())

				if up.jobSetUpdateFn != nil {
					up.jobSetUpdateFn(&jobSet)
				} else if up.jobUpdateFn != nil {
					// Fetch updated job objects so we always have the latest resource versions to perform mutations on.
					var jobList batchv1.JobList
					gomega.Eventually(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)), timeout, interval).Should(gomega.Succeed())
					gomega.Expect(len(jobList.Items)).To(gomega.Equal(testutil.NumExpectedJobs(js)))
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
						TargetReplicatedJobs: []string{"replicated-job-a"},
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
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are recreated")
						gomega.Eventually(checkJobsRecreated, timeout, interval).WithArguments(js, 1).Should(gomega.Equal(true))
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
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are recreated")
						gomega.Eventually(checkJobsRecreated, timeout, interval).WithArguments(js, 1).Should(gomega.Equal(true))
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
						ginkgo.By("checking all jobs are resumed")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
					checkJobSetCondition: testutil.JobSetResumed,
				},
				{
					checkJobState: func(js *jobset.JobSet) {
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
						gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: controllers.GenSubdomain(js, &js.Spec.ReplicatedJobs[1]), Namespace: js.Namespace}, &svc)).Should(gomega.Succeed())
						gomega.Expect(k8sClient.Delete(ctx, &svc)).Should(gomega.Succeed())
					},
					// Service should be recreated during reconciliation.
					checkJobSetState: checkExpectedServices,
				},
			},
		}),
	) // end of DescribeTable
}) // end of Describe

func numExpectedServices(js *jobset.JobSet) int {
	// Expect 1 headless service per replicatedJob.
	expected := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		if rjob.Network != nil && rjob.Network.EnableDNSHostnames != nil && *rjob.Network.EnableDNSHostnames {
			expected += 1
		}
	}
	return expected
}

func completeAllJobs(jobList *batchv1.JobList) {
	ginkgo.By("completing all jobs")
	for _, job := range jobList.Items {
		completeJob(&job)
	}
}

func completeJob(stalejob *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("completing job: %s", stalejob.Name))

	gomega.Eventually(func() error {
		var jobGet batchv1.Job
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: stalejob.Name, Namespace: stalejob.Namespace}, &jobGet); err != nil {
			return err
		}
		jobGet.Status.Conditions = append(jobGet.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		})
		return k8sClient.Status().Update(ctx, &jobGet)
	}, timeout, interval).Should(gomega.Succeed())
}

func failJob(stalejob *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("failing job: %s", stalejob.Name))
	gomega.Eventually(func() error {
		var jobGet batchv1.Job
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: stalejob.Name, Namespace: stalejob.Namespace}, &jobGet); err != nil {
			return err
		}
		jobGet.Status.Conditions = append(jobGet.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		})
		return k8sClient.Status().Update(ctx, &jobGet)
	}, timeout, interval).Should(gomega.Succeed())
}

func suspendJobSet(js *jobset.JobSet, suspend bool) {
	gomega.Eventually(func() error {
		var jobset jobset.JobSet
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset); err != nil {
			return err
		}
		jobset.Spec.Suspend = pointer.Bool(suspend)
		return k8sClient.Update(ctx, &jobset)
	}, timeout, interval).Should(gomega.Succeed())
}

func updateJobSetNodeSelectors(js *jobset.JobSet, nodeSelectors map[string]map[string]string) {
	for index := range js.Spec.ReplicatedJobs {
		js.Spec.ReplicatedJobs[index].
			Template.Spec.Template.Spec.NodeSelector = nodeSelectors[js.Name+"-"+js.Spec.ReplicatedJobs[index].Name]
	}
	gomega.Eventually(k8sClient.Update(ctx, js), timeout, interval).Should(gomega.Succeed())
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
		nodeSelectorKey, ok := job.Labels[jobset.ReplicatedJobNameKey]
		if !ok {
			return false, fmt.Errorf(fmt.Sprintf("%s job missing ReplicatedJobName label", job.Name))
		}
		if !apiequality.Semantic.DeepEqual(job.Spec.Template.Spec.NodeSelector, nodeSelectors[nodeSelectorKey]) {
			return false, nil
		}
		jobsUpdated++
	}
	// Calculate expected number of updated jobs
	wantJobsUpdated := 0
	for _, rj := range js.Spec.ReplicatedJobs {
		if _, exists := nodeSelectors[rj.Name]; exists {
			wantJobsUpdated += rj.Replicas
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
		if job.Labels[controllers.RestartsKey] != strconv.Itoa(expectedRestarts) {
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

// 2 replicated jobs:
// - one with 1 replica
// - one with 3 replicas and DNS hostnames enabled
func testJobSet(ns *corev1.Namespace) *testing.JobSetWrapper {
	return testing.MakeJobSet("test-js", ns.Name).
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
			Job(testing.MakeJobTemplate("test-job-A", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
			Replicas(1).
			Obj()).
		ReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
			Job(testing.MakeJobTemplate("test-job-B", ns.Name).PodSpec(testing.TestPodSpec).CompletionMode(batchv1.IndexedCompletion).Obj()).
			EnableDNSHostnames(true).
			Replicas(3).
			Obj())
}
