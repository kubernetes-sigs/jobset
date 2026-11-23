/*
Copyright The Kubernetes Authors.

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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	testutil "sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("JobSet cache", func() {
	ginkgo.It("filters both the initial list and watched Pods and Jobs", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "cache-test-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		createObjects := func(name string, labelSet map[string]string) []client.Object {
			podSpec := corev1.PodSpec{
				Containers:    []corev1.Container{{Name: "test", Image: "test-image"}},
				RestartPolicy: corev1.RestartPolicyNever,
			}
			objects := []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: labelSet},
					Spec:       podSpec,
				},
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: labelSet},
					Spec:       batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: podSpec}},
				},
			}
			for _, obj := range objects {
				gomega.Expect(k8sClient.Create(ctx, obj)).To(gomega.Succeed())
			}
			return objects
		}

		jobSetLabels := map[string]string{jobset.JobSetNameKey: "test-jobset"}
		unrelated := createObjects("unrelated", nil)
		createObjects("initial", jobSetLabels)

		options := testutil.JobSetCacheOptions()
		options.Scheme = scheme.Scheme
		filteredCache, err := cache.New(cfg, options)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(controllers.SetupJobSetIndexes(ctx, filteredCache)).To(gomega.Succeed())
		gomega.Expect(controllers.SetupPodIndexes(ctx, filteredCache)).To(gomega.Succeed())

		cacheCtx, stopCache := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- filteredCache.Start(cacheCtx) }()
		ginkgo.DeferCleanup(func() {
			stopCache()
			gomega.Eventually(done, timeout, interval).Should(gomega.Receive(gomega.BeNil()))
		})
		syncCtx, stopSync := context.WithTimeout(ctx, timeout)
		defer stopSync()
		gomega.Expect(filteredCache.WaitForCacheSync(syncCtx)).To(gomega.BeTrue())

		expectNames := func(names ...string) {
			gomega.Eventually(func(g gomega.Gomega) {
				var pods corev1.PodList
				g.Expect(filteredCache.List(ctx, &pods, client.InNamespace(ns.Name))).To(gomega.Succeed())
				g.Expect(pods.Items).To(gomega.WithTransform(func(items []corev1.Pod) []string {
					var result []string
					for _, pod := range items {
						result = append(result, pod.Name)
					}
					return result
				}, gomega.ConsistOf(names)))
				var jobs batchv1.JobList
				g.Expect(filteredCache.List(ctx, &jobs, client.InNamespace(ns.Name))).To(gomega.Succeed())
				g.Expect(jobs.Items).To(gomega.WithTransform(func(items []batchv1.Job) []string {
					var result []string
					for _, job := range items {
						result = append(result, job.Name)
					}
					return result
				}, gomega.ConsistOf(names)))
			}, timeout, interval).Should(gomega.Succeed())
		}

		expectNames("initial")
		for _, obj := range unrelated {
			gomega.Expect(apierrors.IsNotFound(filteredCache.Get(ctx, client.ObjectKeyFromObject(obj), obj.DeepCopyObject().(client.Object)))).To(gomega.BeTrue())
		}

		createObjects("watched", jobSetLabels)
		createObjects("watched-unrelated", nil)
		expectNames("initial", "watched")

		// Crossing the selector boundary must add and remove objects from the cache.
		for _, obj := range unrelated {
			obj.SetLabels(jobSetLabels)
			gomega.Expect(k8sClient.Update(ctx, obj)).To(gomega.Succeed())
		}
		expectNames("initial", "watched", "unrelated")
		for _, obj := range unrelated {
			obj.SetLabels(nil)
			gomega.Expect(k8sClient.Update(ctx, obj)).To(gomega.Succeed())
		}
		expectNames("initial", "watched")
	})
})
