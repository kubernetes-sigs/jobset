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

package test

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
	"sigs.k8s.io/jobset/pkg/util/testing"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

var _ = ginkgo.Describe("JobSet controller", func() {

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
		// Delete test namespace after each test.
		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	// jobSetUpdate contains the mutations to perform on the jobset and the
	// checks to perform afterwards.
	type jobSetUpdate struct {
		jobUpdateFn             func(*batchv1.JobList)
		checkJobSetState        func(*jobset.JobSet)
		expectedJobSetCondition jobset.JobSetConditionType
	}

	type testCase struct {
		makeJobSet               func(*corev1.Namespace) *testing.JobSetWrapper
		jobSetCreationShouldFail bool
		updates                  []*jobSetUpdate
	}

	ginkgo.DescribeTable("jobset is created and its jobs go through a series of updates",
		func(tc *testCase) {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := tc.makeJobSet(ns).Obj()

			// If we are expected a validation error creating the jobset, end the test early.
			if tc.jobSetCreationShouldFail {
				ginkgo.By("checking that jobset creation fails")
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Not(gomega.Succeed()))
				return
			}

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(checkNumJobs, timeout, interval).WithArguments(ctx, js).Should(gomega.Equal(numExpectedJobs(js)))

			// Perform a series of updates to jobset resources and check resulting jobset state after each update.
			for _, update := range tc.updates {

				// Fetch updated job objects so we always have the latest resource versions to perform mutations on.
				var jobList batchv1.JobList
				gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())

				// Perform mutation on jobset if specified.
				if update.jobUpdateFn != nil {
					update.jobUpdateFn(&jobList)
				}

				// Check jobset state if specified.
				if update.checkJobSetState != nil {
					update.checkJobSetState(js)
				}

				// Check jobset status if specified.
				if update.expectedJobSetCondition != "" {
					ginkgo.By(fmt.Sprintf("checking jobset status is: %s", update.expectedJobSetCondition))
					gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, update.expectedJobSetCondition).Should(gomega.Equal(true))
				}
			}
		},
		// TODO: move validation tests to separate Describe + DescribeTable
		ginkgo.Entry("validate enableDNSHostnames can't be set if job is not Indexed", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					AddReplicatedJob(testing.MakeReplicatedJob("test-job").
						SetJob(testing.MakeJobTemplate("test-job", ns.Name).Obj()).
						SetEnableDNSHostnames(true).
						Obj())
			},
			jobSetCreationShouldFail: true,
		}),
		ginkgo.Entry("jobset should succeed after all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					jobUpdateFn:             completeAllJobs,
					expectedJobSetCondition: jobset.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("jobset with no failure policy should fail if any jobs fail", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					expectedJobSetCondition: jobset.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("jobset with DNS hostnames enabled should created 1 headless service per job and succeed when all jobs succeed", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn:             completeAllJobs,
					expectedJobSetCondition: jobset.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("succeeds from first run", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn:             completeAllJobs,
					expectedJobSetCondition: jobset.JobSetCompleted,
				},
			},
		}),
		ginkgo.Entry("fails from first run, no restarts", &testCase{
			makeJobSet: testJobSet,
			updates: []*jobSetUpdate{
				{
					checkJobSetState: checkExpectedServices,
				},
				{
					jobUpdateFn: func(jobList *batchv1.JobList) {
						failJob(&jobList.Items[0])
					},
					expectedJobSetCondition: jobset.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("jobset fails after reaching max restarts", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SetFailurePolicy(&jobset.FailurePolicy{
						Operator:      jobset.TerminationPolicyTargetAny,
						RestartPolicy: jobset.RestartPolicyRecreateAll,
						MaxRestarts:   1,
					})
			},
			updates: []*jobSetUpdate{
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
					expectedJobSetCondition: jobset.JobSetFailed,
				},
			},
		}),
		ginkgo.Entry("job succeeds after one failure", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testJobSet(ns).
					SetFailurePolicy(&jobset.FailurePolicy{
						Operator:      jobset.TerminationPolicyTargetAny,
						RestartPolicy: jobset.RestartPolicyRecreateAll,
						MaxRestarts:   1,
					})
			},
			updates: []*jobSetUpdate{
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
					jobUpdateFn:             completeAllJobs,
					expectedJobSetCondition: jobset.JobSetCompleted,
				},
			},
		}),
	) // end of DescribeTable
}) // end of Describe

func checkJobSetStatus(js *jobset.JobSet, condition jobset.JobSetConditionType) (bool, error) {
	var fetchedJS jobset.JobSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, &fetchedJS); err != nil {
		return false, err
	}
	for _, c := range fetchedJS.Status.Conditions {
		if c.Type == string(condition) {
			return true, nil
		}
	}
	return false, nil
}

func numExpectedJobs(js *jobset.JobSet) int {
	expectedJobs := 0
	for _, rjob := range js.Spec.Jobs {
		expectedJobs += rjob.Replicas
	}
	return expectedJobs
}

func numExpectedServices(js *jobset.JobSet) int {
	expectedJobs := 0
	for _, rjob := range js.Spec.Jobs {
		if rjob.Network != nil && rjob.Network.EnableDNSHostnames != nil && *rjob.Network.EnableDNSHostnames {
			expectedJobs += rjob.Replicas
		}
	}
	return expectedJobs
}

func completeAllJobs(jobList *batchv1.JobList) {
	ginkgo.By("completing all jobs")
	for _, job := range jobList.Items {
		completeJob(&job)
	}
}

func completeJob(job *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("completing job: %s", job.Name))
	job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
		Type:   batchv1.JobComplete,
		Status: corev1.ConditionTrue,
	})
	gomega.Expect(k8sClient.Status().Update(ctx, job)).Should(gomega.Succeed())
}

func failJob(job *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("failing job: %s", job.Name))
	job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
		Type:   batchv1.JobFailed,
		Status: corev1.ConditionTrue,
	})
	gomega.Expect(k8sClient.Status().Update(ctx, job)).Should(gomega.Succeed())
}

func checkJobsRecreated(js *jobset.JobSet, expectedRestarts int) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != numExpectedJobs(js) {
		return false, nil
	}
	// Check all the jobs restart counter has been incremented.
	for _, job := range jobList.Items {
		if job.Labels[jobset.RestartsLabel] != strconv.Itoa(expectedRestarts) {
			return false, nil
		}
	}
	return true, nil
}

func checkNumJobs(ctx context.Context, js *jobset.JobSet) (int, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return -1, err
	}
	return len(jobList.Items), nil
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
	return testing.MakeJobSet("js-succeed", ns.Name).
		AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
			SetJob(testing.MakeJobTemplate("test-job-A", ns.Name).Obj()).
			SetReplicas(1).
			Obj()).
		AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
			SetJob(testing.MakeJobTemplate("test-job-B", ns.Name).SetCompletionMode(batchv1.IndexedCompletion).Obj()).
			SetEnableDNSHostnames(true).
			SetReplicas(3).
			Obj())
}
