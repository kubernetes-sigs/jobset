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

	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		// Create a new namespace for each test.
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
		// Delete namespace created for test case after each test.
		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("a jobset is created without DNS hostnames enabled", func() {
		ginkgo.It("should create all jobs and complete successfully once all jobs are completed", func() {
			ginkgo.By("creating a new JobSet")
			ctx := context.Background()
			// Construct JobSet with 3 replicated jobs with only 1 replica each.
			js := testing.MakeJobSet("js-succeed", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
					SetJob(testing.MakeJob("test-job-A", ns.Name).Obj()).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
					SetJob(testing.MakeJob("test-job-B", ns.Name).Obj()).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-c").
					SetJob(testing.MakeJob("test-job-C", ns.Name).Obj()).
					Obj()).
				Obj()

			// Create the JobSet.
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet eventually has 3 active jobs")
			var childJobList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobList.Items), nil
			}, timeout, interval).Should(gomega.Equal(3))

			ginkgo.By("checking JobSet status is completed once all its jobs are completed")
			// Mark jobs as complete.
			for _, job := range childJobList.Items {
				job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				})
				gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())
			}
			// Check JobSet has completed.
			gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetCompleted).Should(gomega.Equal(true))
		})

		ginkgo.It("should create all jobs and fail if any job fails", func() {
			ginkgo.By("creating a new JobSet")
			// Construct JobSet with 3 replicated jobs with only 1 replica each.
			js := testing.MakeJobSet("js-fail", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
					SetJob(testing.MakeJob("test-job", ns.Name).Obj()).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
					SetJob(testing.MakeJob("test-job", ns.Name).Obj()).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-c").
					SetJob(testing.MakeJob("test-job", ns.Name).Obj()).
					Obj()).
				Obj()
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet eventually has 3 active jobs")
			var childJobsList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobsList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobsList.Items), nil
			}, timeout, interval).Should(gomega.Equal(3))

			ginkgo.By("checking JobSet status is failed once 1 job fails")
			// Mark 1 job as failed.
			job := childJobsList.Items[0]
			job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
				Type:   batchv1.JobFailed,
				Status: corev1.ConditionTrue,
			})
			gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())

			// Check JobSet has failed.
			gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetFailed).Should(gomega.Equal(true))
		})
	})

	ginkgo.When("a jobset is created with DNS hostnames enabled", func() {
		ginkgo.It("should create all jobs and headless services, then complete successfully once all jobs are completed", func() {
			ginkgo.By("creating a new JobSet")
			ctx := context.Background()

			// Construct JobSet with 3 replicated jobs with only 1 replica each and pod DNS hostnames enabled.
			js := testing.MakeJobSet("js-hostnames", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-a").
					SetJob(testing.MakeJob("test-job", ns.Name).SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetEnableDNSHostnames(true).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-b").
					SetJob(testing.MakeJob("test-job", ns.Name).SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetEnableDNSHostnames(true).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-c").
					SetJob(testing.MakeJob("test-job", ns.Name).SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetEnableDNSHostnames(true).
					Obj()).
				Obj()

			// Create JobSet.
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet eventually has 3 active jobs")
			var childJobList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobList.Items), nil
			}, timeout, interval).Should(gomega.Equal(3))

			ginkgo.By("checking JobSet eventually has 3 headless services")
			gomega.Eventually(func() (int, error) {
				var svcList corev1.ServiceList
				if err := k8sClient.List(ctx, &svcList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(svcList.Items), nil
			}).Should(gomega.Equal(3))

			ginkgo.By("checking JobSet status is completed once all its jobs are completed")
			// Mark jobs as complete.
			for _, job := range childJobList.Items {
				job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				})
				gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())
			}
			// Check JobSet has completed.
			gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetCompleted).Should(gomega.Equal(true))
		})

		ginkgo.It("jobset validation should fail if job completion mode is not indexed", func() {
			ginkgo.By("creating a new JobSet")
			// Construct JobSet with 3 replicated jobs with only 1 replica each.
			js := testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("test-job").
					SetJob(testing.MakeJob("test-job", ns.Name).Obj()).
					SetEnableDNSHostnames(true).
					Obj()).Obj()
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Not(gomega.Succeed()))
		})
	})

	ginkgo.When("a jobset is created with 2 replicated jobs with 3 replicas each and pod DNS hostnames enabled", func() {
		ginkgo.It("should create all jobs and services with the correct number of replicas, then complete successfully once all jobs are completed", func() {
			ginkgo.By("creating a new JobSet")
			ctx := context.Background()

			// Construct JobSet with 2 replicated jobs with 3 replicas each.
			js := testing.MakeJobSet("js-2-rjobs-3-replicas", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-foo").
					SetJob(testing.MakeJob("test-job-foo", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					SetEnableDNSHostnames(true).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-bar").
					SetJob(testing.MakeJob("test-job-bar", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					SetEnableDNSHostnames(true).
					Obj()).
				Obj()

			// Create JobSet.
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet eventually has 6 active jobs")
			var childJobList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobList.Items), nil
			}, timeout, interval).Should(gomega.Equal(6))

			ginkgo.By("checking JobSet eventually has 6 headless services")
			gomega.Eventually(func() (int, error) {
				var svcList corev1.ServiceList
				if err := k8sClient.List(ctx, &svcList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(svcList.Items), nil
			}).Should(gomega.Equal(6))

			ginkgo.By("checking JobSet status is completed once all its jobs are completed")
			// Mark jobs as complete.
			for _, job := range childJobList.Items {
				job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				})
				gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())
			}
			// Check JobSet has completed.
			gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetCompleted).Should(gomega.Equal(true))
		})

		ginkgo.It("should create all jobs with the correct number of replicas and fail if any job fails", func() {
			ginkgo.By("creating a new JobSet")
			// Construct JobSet with 2 replicated jobs with 3 replicas each.
			js := testing.MakeJobSet("js-2-rjobs-3-replicas", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-foo").
					SetJob(testing.MakeJob("test-job-foo", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					SetEnableDNSHostnames(true).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-bar").
					SetJob(testing.MakeJob("test-job-bar", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					SetEnableDNSHostnames(true).
					Obj()).
				Obj()

			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet eventually has 6 active jobs")
			var childJobsList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobsList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobsList.Items), nil
			}, timeout, interval).Should(gomega.Equal(6))

			ginkgo.By("checking JobSet status is failed once 1 job fails")
			// Mark 1 job as failed.
			job := childJobsList.Items[0]
			job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
				Type:   batchv1.JobFailed,
				Status: corev1.ConditionTrue,
			})
			gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())

			// Check JobSet has failed.
			gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetFailed).Should(gomega.Equal(true))
		})
	})

	// jobSetUpdate contains the mutations to perform on the jobset and the
	// checks to perform afterwards.
	type jobSetUpdate struct {
		name                    string
		jobUpdateFn             func(jobList *batchv1.JobList) error
		checkJobSetState        func(js *jobset.JobSet) (bool, error)
		expectedJobSetCondition jobset.JobSetConditionType
	}

	ginkgo.DescribeTable("a jobset is created with failure policy 'Any' and restart policy 'RecreateAll'",
		func(updates []*jobSetUpdate) {
			ginkgo.By("creating a new JobSet")
			ctx := context.Background()

			// Construct JobSet with 2 replicated jobs with 3 replicas each.
			js := testing.MakeJobSet("js-failure-policy-any-with-recreate", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-leader").
					SetJob(testing.MakeJob("test-job-leader", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-worker").
					SetJob(testing.MakeJob("test-job-worker", ns.Name).
						SetCompletionMode(batchv1.IndexedCompletion).Obj()).
					SetReplicas(3).
					Obj()).
				// Set failure policy to "Any" with restart policy "Recreate" with max 1 restart.
				SetFailurePolicy(&jobset.FailurePolicy{
					Operator:      jobset.TerminationPolicyTargetAny,
					RestartPolicy: jobset.RestartPolicyRecreateAll,
					MaxRestarts:   1,
				}).
				Obj()

			// Create JobSet.
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			// Check all jobs are created successfully.
			ginkgo.By("checking JobSet eventually has 6 active jobs")
			var originalJobList batchv1.JobList
			gomega.Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &originalJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(originalJobList.Items), nil
			}, timeout, interval).Should(gomega.Equal(6))

			// Run each update job function and check resulting jobset state afterwards.
			for _, update := range updates {
				// Refresh jobList before every update, so we have access to the currently existing jobs.
				var jobList batchv1.JobList
				gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())

				ginkgo.By("updating job(s)")
				gomega.Expect(update.jobUpdateFn(&jobList)).Should(gomega.Succeed())

				if update.checkJobSetState != nil {
					ginkgo.By("checking jobset state")
					gomega.Eventually(update.checkJobSetState, timeout, interval).WithArguments(js).Should(gomega.Equal(true))
				}

				if update.expectedJobSetCondition != "" {
					ginkgo.By("checking jobset status")
					gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(js, update.expectedJobSetCondition).Should(gomega.Equal(true))
				}
			}
		},
		ginkgo.Entry("jobset fails if attempting to exceed max restarts", []*jobSetUpdate{
			{
				jobUpdateFn: func(jobList *batchv1.JobList) error {
					ginkgo.By("failing a job")
					job := &jobList.Items[0]
					job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
					})
					gomega.Expect(k8sClient.Status().Update(ctx, job)).Should(gomega.Succeed())
					return nil
				},
				checkJobSetState: func(js *jobset.JobSet) (bool, error) {
					ginkgo.By("checking all jobs are recreated")
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
						if job.Labels[jobset.RestartsLabel] != "1" {
							return false, nil
						}
					}
					return true, nil
				},
			},
			{
				jobUpdateFn: func(jobList *batchv1.JobList) error {
					ginkgo.By("failing another job")
					job := &jobList.Items[0]
					job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
					})
					gomega.Expect(k8sClient.Status().Update(ctx, job)).Should(gomega.Succeed())
					return nil
				},
				expectedJobSetCondition: jobset.JobSetFailed,
			},
		}),
		ginkgo.Entry("1 job succeeds 1 job fails, all jobs recreated", []*jobSetUpdate{
			{
				jobUpdateFn: func(jobList *batchv1.JobList) error {
					ginkgo.By("succeeding a job")
					job := jobList.Items[0]
					job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
						Type:   batchv1.JobComplete,
						Status: corev1.ConditionTrue,
					})
					gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())

					ginkgo.By("failing a job")
					job = jobList.Items[1]
					job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
					})
					gomega.Expect(k8sClient.Status().Update(ctx, &job)).Should(gomega.Succeed())
					return nil
				},
				checkJobSetState: func(js *jobset.JobSet) (bool, error) {
					ginkgo.By("checking all jobs are recreated")
					var jobList batchv1.JobList
					if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
						return false, err
					}
					// Check we have the right number of jobs.
					if len(jobList.Items) != numExpectedJobs(js) {
						fmt.Fprintf(ginkgo.GinkgoWriter, fmt.Sprintf("numJobs: %d, expected: %d\n", len(jobList.Items), numExpectedJobs(js)))
						return false, nil
					}
					// Check all the jobs restart counter has been incremented.
					for _, job := range jobList.Items {
						if job.Labels[jobset.RestartsLabel] != "1" {
							fmt.Fprintf(ginkgo.GinkgoWriter, fmt.Sprintf("job: %s, restarts: %s\n", job.Name, job.Labels[jobset.RestartsLabel]))
							return false, nil
						}
					}
					return true, nil
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
