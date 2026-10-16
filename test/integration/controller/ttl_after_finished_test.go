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
	"path/filepath"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

// ttlCachedClient simulates an informer that has not observed the latest write.
// Writes still go to the API server so deletion preconditions are enforced there.
type ttlCachedClient struct {
	client.Client
	cached *jobset.JobSet
}

func (c *ttlCachedClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if js, ok := obj.(*jobset.JobSet); ok && key == client.ObjectKeyFromObject(c.cached) {
		c.cached.DeepCopyInto(js)
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type ttlStaleReconcileCase struct {
	change             func(*jobset.JobSet)
	recreate           bool
	wantConflict       bool
	wantDeletedOnRetry bool
	wantRequeue        bool
}

var _ = ginkgo.Describe("TTLStaleReconcile", ginkgo.Ordered, ginkgo.ContinueOnFailure, func() {
	var (
		ttlCtx    context.Context
		ttlClient client.Client
		ttlScheme *runtime.Scheme
		ns        *corev1.Namespace
	)

	ginkgo.BeforeAll(func() {
		// The suite's background controller still honors TTL for externally
		// managed JobSets. Keep this environment isolated so that stale reads
		// and concurrent API updates occur in a deterministic order.
		ttlEnv := &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "components", "crd", "bases")},
			ErrorIfCRDPathMissing: true,
		}
		config, err := ttlEnv.Start()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(ttlEnv.Stop()).To(gomega.Succeed())
		})

		ttlScheme = runtime.NewScheme()
		gomega.Expect(corev1.AddToScheme(ttlScheme)).To(gomega.Succeed())
		gomega.Expect(jobset.AddToScheme(ttlScheme)).To(gomega.Succeed())
		ttlClient, err = client.New(config, client.Options{Scheme: ttlScheme})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ttlCtx = context.Background()
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "ttl-stale-"}}
		gomega.Expect(ttlClient.Create(ttlCtx, ns)).To(gomega.Succeed())
	})

	ginkgo.DescribeTable("reconciles using an outdated expiration decision", func(tc ttlStaleReconcileCase) {
		js := testutils.MakeJobSet("", ns.Name).ManagedBy("other-controller").TTLSecondsAfterFinished(0).Obj()
		js.GenerateName = "expired-"
		gomega.Expect(ttlClient.Create(ttlCtx, js)).To(gomega.Succeed())
		js.Status.Conditions = []metav1.Condition{{Type: string(jobset.JobSetCompleted), Status: metav1.ConditionTrue, Reason: "ByTest", LastTransitionTime: metav1.Now()}}
		gomega.Expect(ttlClient.Status().Update(ttlCtx, js)).To(gomega.Succeed())
		cached := js.DeepCopy()
		if tc.recreate {
			gomega.Expect(ttlClient.Delete(ttlCtx, js)).To(gomega.Succeed())
			gomega.Eventually(func() bool {
				return apierrors.IsNotFound(ttlClient.Get(ttlCtx, client.ObjectKeyFromObject(js), &jobset.JobSet{}))
			}, timeout, interval).Should(gomega.BeTrue())
			replacement := testutils.MakeJobSet(js.Name, js.Namespace).ManagedBy("other-controller").Obj()
			gomega.Expect(ttlClient.Create(ttlCtx, replacement)).To(gomega.Succeed())
		} else if tc.change != nil {
			tc.change(js)
			gomega.Expect(ttlClient.Update(ttlCtx, js)).To(gomega.Succeed())
		}

		cachedClient := &ttlCachedClient{Client: ttlClient, cached: cached}
		r := controllers.NewJobSetReconciler(cachedClient, ttlScheme, nil)
		_, err := r.Reconcile(ttlCtx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cached)})
		if !tc.wantConflict {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			fresh := &jobset.JobSet{}
			err = ttlClient.Get(ttlCtx, client.ObjectKeyFromObject(cached), fresh)
			gomega.Expect(apierrors.IsNotFound(err) || (err == nil && !fresh.DeletionTimestamp.IsZero())).To(gomega.BeTrue(), "expired JobSet must be deleted")
			return
		}

		gomega.Expect(apierrors.IsConflict(err)).To(gomega.BeTrue(), "expected a conflict, got %v", err)
		fresh := &jobset.JobSet{}
		gomega.Expect(ttlClient.Get(ttlCtx, client.ObjectKeyFromObject(cached), fresh)).To(gomega.Succeed())
		gomega.Expect(fresh.DeletionTimestamp.IsZero()).To(gomega.BeTrue(), "latest JobSet must be retained")
		cachedClient.cached = fresh
		result, err := r.Reconcile(ttlCtx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(fresh)})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(result.RequeueAfter > 0).To(gomega.Equal(tc.wantRequeue), "the latest TTL must be honored")
		latest := &jobset.JobSet{}
		err = ttlClient.Get(ttlCtx, client.ObjectKeyFromObject(fresh), latest)
		if tc.wantDeletedOnRetry {
			gomega.Expect(apierrors.IsNotFound(err) || (err == nil && !latest.DeletionTimestamp.IsZero())).To(gomega.BeTrue(), "still-expired JobSet must be deleted on retry")
		} else {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(latest.DeletionTimestamp.IsZero()).To(gomega.BeTrue(), "JobSet must also be retained on retry")
		}
	},
		ginkgo.Entry("unchanged expired object", ttlStaleReconcileCase{}),
		ginkgo.Entry("TTL extended", ttlStaleReconcileCase{
			change:       func(js *jobset.JobSet) { js.Spec.TTLSecondsAfterFinished = ptr.To[int32](3600) },
			wantConflict: true,
			wantRequeue:  true,
		}),
		ginkgo.Entry("metadata updated", ttlStaleReconcileCase{
			change:             func(js *jobset.JobSet) { js.Labels = map[string]string{"test": "updated"} },
			wantConflict:       true,
			wantDeletedOnRetry: true,
		}),
		ginkgo.Entry("TTL removed", ttlStaleReconcileCase{
			change:       func(js *jobset.JobSet) { js.Spec.TTLSecondsAfterFinished = nil },
			wantConflict: true,
		}),
		ginkgo.Entry("same name recreated", ttlStaleReconcileCase{
			recreate:     true,
			wantConflict: true,
		}),
	)
})
