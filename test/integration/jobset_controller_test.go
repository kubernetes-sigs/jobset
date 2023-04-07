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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

type jobSetArgs struct {
	name               string
	namespace          string
	numJobs            int
	enableDNSHostnames bool
}

var _ = Describe("JobSet controller", func() {

	var ns *corev1.Namespace

	BeforeEach(func() {
		// Create a new namespace for each test.
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Wait for namespace to exist before proceeding with test.
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
			if err != nil {
				return false
			}
			return true
		}, timeout, interval).Should(BeTrue())
	})

	AfterEach(func() {
		// Delete namespace created for test case after each test.
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	When("a jobset is created without DNS hostnames enabled", func() {
		It("should create all jobs and complete successfully once all jobs are completed", func() {
			By("creating a new JobSet")
			ctx := context.Background()
			js := constructJobSet(&jobSetArgs{
				name:      "js-simple",
				namespace: ns.Name,
				numJobs:   3,
			})
			Expect(k8sClient.Create(ctx, js)).Should(Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			By("checking JobSet was created successfully")
			Eventually(jobSetExists, timeout, interval).WithArguments(js).Should(BeTrue())

			By("checking JobSet eventually has 3 active jobs")
			var childJobList batchv1.JobList
			Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobList.Items), nil
			}, timeout, interval).Should(Equal(3))

			By("checking JobSet status is completed once all its jobs are completed")
			// Mark jobs as complete.
			for _, job := range childJobList.Items {
				job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				})
				Expect(k8sClient.Status().Update(ctx, &job)).Should(Succeed())
			}
			// Check JobSet has completed.
			Eventually(assertJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetCompleted).Should(Equal(true))
		})

		It("should create all jobs and fail if any job fails", func() {
			By("creating a new JobSet")
			js := constructJobSet(&jobSetArgs{
				name:      "js-failed",
				namespace: ns.Name,
				numJobs:   3,
			})
			Expect(k8sClient.Create(ctx, js)).Should(Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			By("checking JobSet was created successfully")
			Eventually(jobSetExists, timeout, interval).WithArguments(js).Should(BeTrue())

			By("checking JobSet eventually has 3 active jobs")
			var childJobsList batchv1.JobList
			Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobsList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobsList.Items), nil
			}, timeout, interval).Should(Equal(3))

			By("checking JobSet status is failed once 1 job fails")
			// Mark 1 job as failed.
			job := childJobsList.Items[0]
			job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
				Type:   batchv1.JobFailed,
				Status: corev1.ConditionTrue,
			})
			Expect(k8sClient.Status().Update(ctx, &job)).Should(Succeed())

			// Check JobSet has failed.
			Eventually(assertJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetFailed).Should(Equal(true))
		})
	})

	When("a jobset is created with DNS hostnames enabled", func() {
		It("should create all jobs and headless services, then complete successfully once all jobs are completed", func() {
			By("creating a new JobSet")
			ctx := context.Background()

			// Create JobSet.
			js := constructJobSet(&jobSetArgs{
				name:               "js-enable-hostnames",
				namespace:          ns.Name,
				numJobs:            3,
				enableDNSHostnames: true,
			})
			Expect(k8sClient.Create(ctx, js)).Should(Succeed())

			// We'll need to retry getting this newly created JobSet, given that creation may not immediately happen.
			By("checking JobSet was created successfully")
			Eventually(jobSetExists, timeout, interval).WithArguments(js).Should(BeTrue())

			By("checking JobSet eventually has 3 active jobs")
			var childJobList batchv1.JobList
			Eventually(func() (int, error) {
				if err := k8sClient.List(ctx, &childJobList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(childJobList.Items), nil
			}, timeout, interval).Should(Equal(3))

			By("checking JobSet eventually has 3 headless services")
			Eventually(func() (int, error) {
				var svcList corev1.ServiceList
				if err := k8sClient.List(ctx, &svcList, client.InNamespace(js.Namespace)); err != nil {
					return -1, err
				}
				return len(svcList.Items), nil
			}).Should(Equal(3))

			By("checking JobSet status is completed once all its jobs are completed")
			// Mark jobs as complete.
			for _, job := range childJobList.Items {
				job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				})
				Expect(k8sClient.Status().Update(ctx, &job)).Should(Succeed())
			}
			// Check JobSet has completed.
			Eventually(assertJobSetStatus, timeout, interval).WithArguments(js, jobset.JobSetCompleted).Should(Equal(true))
		})
	})
})

func jobSetExists(js *jobset.JobSet) bool {
	err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{})
	if err != nil {
		return false
	}
	return true
}

func assertJobSetStatus(js *jobset.JobSet, condition jobset.JobSetConditionType) (bool, error) {
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

func constructJobSet(args *jobSetArgs) *jobset.JobSet {
	indexedCompletionMode := batchv1.IndexedCompletion
	js := &jobset.JobSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch.x-k8s.io/v1alpha",
			Kind:       "JobSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      args.name,
			Namespace: args.namespace,
		},
		Spec: jobset.JobSetSpec{
			Jobs: []jobset.ReplicatedJob{},
		},
	}
	for i := 0; i < args.numJobs; i++ {
		js.Spec.Jobs = append(js.Spec.Jobs, jobset.ReplicatedJob{
			Name:    fmt.Sprintf("%s-job-template-%d", args.name, i),
			Network: &jobset.Network{EnableDNSHostnames: pointer.Bool(args.enableDNSHostnames)},
			Template: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-job-%d", args.name, i),
					Namespace: args.namespace,
				},
				Spec: batchv1.JobSpec{
					CompletionMode: &indexedCompletionMode,
					Parallelism:    pointer.Int32(1),
					Completions:    pointer.Int32(1),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: "Never",
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "busybox:latest",
								},
							},
						},
					},
				},
			},
		})
	}
	return js
}
