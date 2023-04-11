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
	timeout            = 10 * time.Second
	interval           = time.Millisecond * 250
	consistentDuration = 3 * time.Second
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
				AddReplicatedJobs(testing.MakeReplicatedJob("test-job").
					SetJob(testing.IndexedJob("test-job", ns.Name)).
					Obj(), 3).Obj()

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
				AddReplicatedJobs(testing.MakeReplicatedJob("test-job").
					SetJob(testing.IndexedJob("test-job", ns.Name)).
					Obj(), 3).Obj()
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
				AddReplicatedJobs(testing.MakeReplicatedJob("test-job").
					SetJob(testing.IndexedJob("test-job", ns.Name)).
					SetEnableDNSHostnames(true).
					Obj(), 3).Obj()

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

		ginkgo.It("should throw an error if job completion mode is not indexed", func() {
			ginkgo.By("creating a new JobSet")
			// Construct JobSet with 3 replicated jobs with only 1 replica each.
			js := testing.MakeJobSet("js-hostnames-non-indexed", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("test-job").
					SetJob(testing.Job("test-job", ns.Name)).
					SetEnableDNSHostnames(true).
					Obj()).Obj()
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			ginkgo.By("checking JobSet was created successfully")
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking JobSet consistently has 0 active jobs")
			var childJobsList batchv1.JobList
			gomega.Consistently(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobsList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobsList.Items), nil
			}, consistentDuration, interval).Should(gomega.Equal(0))
		})
	})

	ginkgo.When("a jobset is created with 2 replicated jobs with 3 replicas each and pod DNS hostnames enabled", func() {
		ginkgo.It("should create all jobs and services with the correct number of replicas, then complete successfully once all jobs are completed", func() {
			ginkgo.By("creating a new JobSet")
			ctx := context.Background()

			// Construct JobSet with 2 replicated jobs with 3 replicas each.
			js := testing.MakeJobSet("js-2-rjobs-3-replicas", ns.Name).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-foo").
					SetJob(testing.IndexedJob("test-job-foo", ns.Name)).
					SetReplicas(3).
					SetEnableDNSHostnames(true).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-bar").
					SetJob(testing.IndexedJob("test-job-bar", ns.Name)).
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
					SetJob(testing.IndexedJob("test-job-foo", ns.Name)).
					SetReplicas(3).
					Obj()).
				AddReplicatedJob(testing.MakeReplicatedJob("replicated-job-bar").
					SetJob(testing.IndexedJob("test-job-bar", ns.Name)).
					SetReplicas(3).
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
})

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
