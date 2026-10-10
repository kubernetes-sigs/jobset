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
	"testing"

	"github.com/stretchr/testify/require"
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

func TestTTLAfterFinishedStaleReconcile(t *testing.T) {
	// No background controller runs in this environment: the stale read and
	// concurrent API update must occur in a deterministic order.
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "components", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	config, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, jobset.AddToScheme(scheme))
	c, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "ttl-stale-"}}
	require.NoError(t, c.Create(ctx, ns))

	for _, tc := range []struct {
		name         string
		change       func(*testing.T, *jobset.JobSet)
		wantConflict bool
	}{
		{name: "unchanged expired object"},
		{name: "TTL extended", change: func(t *testing.T, js *jobset.JobSet) { js.Spec.TTLSecondsAfterFinished = ptr.To[int32](3600) }, wantConflict: true},
		{name: "metadata updated", change: func(t *testing.T, js *jobset.JobSet) { js.Labels = map[string]string{"test": "updated"} }, wantConflict: true},
		{name: "TTL removed", change: func(t *testing.T, js *jobset.JobSet) { js.Spec.TTLSecondsAfterFinished = nil }, wantConflict: true},
		{name: "same name recreated", change: func(t *testing.T, js *jobset.JobSet) {
			require.NoError(t, c.Delete(ctx, js))
			require.Eventually(t, func() bool {
				err := c.Get(ctx, client.ObjectKeyFromObject(js), &jobset.JobSet{})
				return apierrors.IsNotFound(err)
			}, timeout, interval)
			replacement := testutils.MakeJobSet(js.Name, js.Namespace).ManagedBy("other-controller").Obj()
			require.NoError(t, c.Create(ctx, replacement))
		}, wantConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			js := testutils.MakeJobSet("", ns.Name).ManagedBy("other-controller").TTLSecondsAfterFinished(0).Obj()
			js.GenerateName = "expired-"
			require.NoError(t, c.Create(ctx, js))
			js.Status.Conditions = []metav1.Condition{{Type: string(jobset.JobSetCompleted), Status: metav1.ConditionTrue, Reason: "ByTest", LastTransitionTime: metav1.Now()}}
			require.NoError(t, c.Status().Update(ctx, js))
			cached := js.DeepCopy()
			if tc.change != nil {
				tc.change(t, js)
				if tc.name != "same name recreated" {
					require.NoError(t, c.Update(ctx, js))
				}
			}
			cachedClient := &ttlCachedClient{Client: c, cached: cached}
			r := controllers.NewJobSetReconciler(cachedClient, scheme, nil)
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cached)})
			if tc.wantConflict {
				require.True(t, apierrors.IsConflict(err), "expected a conflict, got %v", err)
				fresh := &jobset.JobSet{}
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cached), fresh))
				require.True(t, fresh.DeletionTimestamp.IsZero(), "latest JobSet must be retained")
				cachedClient.cached = fresh
				result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(fresh)})
				require.NoError(t, err)
				if tc.name == "TTL extended" {
					require.Positive(t, result.RequeueAfter, "the latest TTL must be honored")
				}
				latest := &jobset.JobSet{}
				err = c.Get(ctx, client.ObjectKeyFromObject(fresh), latest)
				if tc.name == "metadata updated" {
					require.True(t, apierrors.IsNotFound(err) || (err == nil && !latest.DeletionTimestamp.IsZero()), "still-expired JobSet must be deleted on retry")
				} else {
					require.NoError(t, err)
					require.True(t, latest.DeletionTimestamp.IsZero(), "JobSet must also be retained on retry")
				}
			} else {
				require.NoError(t, err)
				fresh := &jobset.JobSet{}
				err = c.Get(ctx, client.ObjectKeyFromObject(cached), fresh)
				require.True(t, apierrors.IsNotFound(err) || (err == nil && !fresh.DeletionTimestamp.IsZero()), "expired JobSet must be deleted")
			}
		})
	}
}
