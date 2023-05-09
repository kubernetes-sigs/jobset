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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 10 * time.Second
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
		existingJob                 func(*corev1.Namespace) *batchv1.Job
		existingService             func(*corev1.Namespace) *corev1.Service
	}

	ginkgo.DescribeTable("JobSet validation during creation and updates",
		func(tc *testCase) {
			ctx := context.Background()

			if tc.existingJob != nil {
				ginkgo.By("create an existing job")
				existingJob := tc.existingJob(ns)
				gomega.Expect(k8sClient.Create(ctx, existingJob)).Should(gomega.Succeed())
				// We'll need to retry getting this newly created job, given that creation may not immediately happen.
				gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: existingJob.Name, Namespace: existingJob.Namespace}, &batchv1.Job{}), timeout, interval).Should(gomega.Succeed())
			}

			if tc.existingService != nil {
				ginkgo.By("create an existing service")
				existingService := tc.existingService(ns)
				gomega.Expect(k8sClient.Create(ctx, existingService)).Should(gomega.Succeed())
				// We'll need to retry getting this newly created job, given that creation may not immediately happen.
				gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: existingService.Name, Namespace: existingService.Namespace}, &corev1.Service{}), timeout, interval).Should(gomega.Succeed())

			}

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
		ginkgo.Entry("validate enableDNSHostnames can't be set if job is not Indexed", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("test-job").
						Job(testing.MakeJobTemplate("test-job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.NonIndexedCompletion).Obj()).
						EnableDNSHostnames(true).
						Obj())
			},
			jobSetCreationShouldSucceed: true,
			updates: []*jobSetUpdate{
				{
					fn: func(js *jobset.JobSet) {
						// Try mutating jobs list.
						js.Spec.ReplicatedJobs = append(js.Spec.ReplicatedJobs, testing.MakeReplicatedJob("test-job").
							Job(testing.MakeJobTemplate("test-job", ns.Name).PodSpec(testing.TestPodSpec).Obj()).
							EnableDNSHostnames(true).
							Obj())
					},
				},
				{
					fn: func(js *jobset.JobSet) {
						// Try mutating failure policy.
						js.Spec.FailurePolicy = &jobset.FailurePolicy{
							MaxRestarts: 3,
						}
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
		ginkgo.Entry("existing job conflicts with jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				ginkgo.By("making js-exist-job")
				return testing.MakeJobSet("js", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("job").
						Job(testing.MakeJobTemplate("test-exist-job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.NonIndexedCompletion).Obj()).
						EnableDNSHostnames(true).
						Replicas(1).
						Obj())
			},
			jobSetCreationShouldSucceed: true,
			existingJob: func(ns *corev1.Namespace) *batchv1.Job {
				return &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-job-0",
						Namespace: ns.Name,
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: testing.TestPodSpec,
						},
					},
				}
			},
		}),
		ginkgo.Entry("existing service conflicts with jobset", &testCase{
			makeJobSet: func(ns *corev1.Namespace) *testing.JobSetWrapper {
				return testing.MakeJobSet("js", ns.Name).
					ReplicatedJob(testing.MakeReplicatedJob("job").
						Job(testing.MakeJobTemplate("job", ns.Name).
							PodSpec(testing.TestPodSpec).
							CompletionMode(batchv1.NonIndexedCompletion).Obj()).
						EnableDNSHostnames(true).
						Replicas(1).
						Obj())
			},
			jobSetCreationShouldSucceed: true,
			existingService: func(ns *corev1.Namespace) *corev1.Service {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-job",
						Namespace: ns.Name,
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "None",
					},
				}
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
			gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(util.NumExpectedJobs(js)))

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
					gomega.Expect(len(jobList.Items)).To(gomega.Equal(util.NumExpectedJobs(js)))
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
					checkJobSetCondition: util.JobSetCompleted,
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
					checkJobSetCondition: util.JobSetActive,
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
					checkJobSetCondition: util.JobSetFailed,
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
					checkJobSetCondition: util.JobSetCompleted,
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
					checkJobSetCondition: util.JobSetCompleted,
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
					checkJobSetCondition: util.JobSetFailed,
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
					checkJobSetCondition: util.JobSetFailed,
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
					checkJobSetCondition: util.JobSetCompleted,
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
					checkJobSetCondition: util.JobSetSuspended,
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
					checkJobSetCondition: util.JobSetSuspended,
				},
				{
					jobSetUpdateFn: func(js *jobset.JobSet) {
						suspendJobSet(js, false)
					},
					checkJobSetState: func(js *jobset.JobSet) {
						ginkgo.By("checking all jobs are resumed")
						gomega.Eventually(matchJobsSuspendState, timeout, interval).WithArguments(js, false).Should(gomega.Equal(true))
					},
					checkJobSetCondition: util.JobSetResumed,
				},
				{
					jobUpdateFn:          completeAllJobs,
					checkJobSetCondition: util.JobSetCompleted,
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
					checkJobSetCondition: util.JobSetSuspended,
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
						gomega.Eventually(k8sClient.Delete(ctx, &svc)).Should(gomega.Succeed())
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

func completeJob(job *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("completing job: %s", job.Name))
	job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
		Type:   batchv1.JobComplete,
		Status: corev1.ConditionTrue,
	})
	gomega.Eventually(k8sClient.Status().Update(ctx, job), timeout, interval).Should(gomega.Succeed())
}

func failJob(job *batchv1.Job) {
	ginkgo.By(fmt.Sprintf("failing job: %s", job.Name))
	job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
		Type:   batchv1.JobFailed,
		Status: corev1.ConditionTrue,
	})
	gomega.Eventually(k8sClient.Status().Update(ctx, job), timeout, interval).Should(gomega.Succeed())
}

func suspendJobSet(js *jobset.JobSet, suspend bool) {
	js.Spec.Suspend = pointer.Bool(suspend)
	gomega.Eventually(k8sClient.Update(ctx, js), timeout, interval).Should(gomega.Succeed())
}

func matchJobsSuspendState(js *jobset.JobSet, suspend bool) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != util.NumExpectedJobs(js) {
		return false, nil
	}

	for _, job := range jobList.Items {
		if *job.Spec.Suspend != suspend {
			return false, nil
		}
	}
	return true, nil
}

func checkJobsRecreated(js *jobset.JobSet, expectedRestarts int) (bool, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return false, err
	}
	// Check we have the right number of jobs.
	if len(jobList.Items) != util.NumExpectedJobs(js) {
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
