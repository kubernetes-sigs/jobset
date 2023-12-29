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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutil "sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("TTLAfterFinished controller", func() {
	ginkgo.It("should delete a JobSet with TTLAfterFinished field set after configured seconds pass", func() {
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
		js := testJobSet(ns).TTLSecondsAfterFinished(5).Obj()

		// Verify jobset created successfully.
		ginkgo.By(fmt.Sprintf("creating jobSet %s/%s", js.Name, js.Namespace))
		gomega.Eventually(k8sClient.Create(ctx, js), timeout, interval).Should(gomega.Succeed())

		ginkgo.By("checking all jobs were created successfully")
		gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

		// Fetch updated job objects, so we always have the latest resource versions to perform mutations on.
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).Should(gomega.Succeed())
		gomega.Expect(len(jobList.Items)).To(gomega.Equal(testutil.NumExpectedJobs(js)))
		completeAllJobs(&jobList)

		// Verify jobset is marked as completed.
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)

		// Verify jobset has not been deleted if ttl has not passed.
		ginkgo.By("checking that jobset has not been deleted before configured seconds pass")
		var fresh jobset.JobSet
		gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), &fresh)).To(gomega.Succeed())
		gomega.Expect(fresh.DeletionTimestamp).To(gomega.BeNil())

		ginkgo.By("checking that ttl after finished controller deletes jobset after configured seconds pass")
		gomega.Eventually(func() bool {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(js), &fresh); err != nil {
				return false
			}
			return !fresh.DeletionTimestamp.IsZero()
		}, 15*time.Second, 500*time.Millisecond).Should(gomega.BeTrue())
	})
})
