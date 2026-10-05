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

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func schedulingTestJobSet() *jobset.JobSet {
	return &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset", Namespace: "default", UID: "jobset-uid"},
		Spec: jobset.JobSetSpec{
			Scheduling:     &jobset.JobSetScheduling{},
			ReplicatedJobs: []jobset.ReplicatedJob{{Name: "workers", Replicas: 1}},
		},
	}
}

func schedulingTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(schedulingv1beta1.AddToScheme(scheme))
	return scheme
}

func TestReconcileSchedulingObjectsRejectsUnownedExistingWorkload(t *testing.T) {
	js := schedulingTestJobSet()
	existing := &schedulingv1beta1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name: workloadName(js), Namespace: js.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: jobset.GroupVersion.String(), Kind: "JobSet", Name: js.Name, UID: "another-uid",
			}},
		},
	}
	scheme := schedulingTestScheme()
	r := &JobSetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(), Scheme: scheme}

	if err := r.reconcileSchedulingObjects(context.Background(), js); err == nil {
		t.Fatal("reconcileSchedulingObjects() returned nil for an unowned existing Workload")
	}
	var podGroups schedulingv1beta1.PodGroupList
	if err := r.List(context.Background(), &podGroups); err != nil {
		t.Fatalf("listing PodGroups: %v", err)
	}
	if len(podGroups.Items) != 0 {
		t.Fatal("PodGroups were created despite the Workload ownership error")
	}
}

func TestReconcileSchedulingObjectsLabelsCreatedWorkload(t *testing.T) {
	js := schedulingTestJobSet()
	scheme := schedulingTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &JobSetReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSchedulingObjects(context.Background(), js); err != nil {
		t.Fatalf("reconcileSchedulingObjects() error = %v", err)
	}

	workload := &schedulingv1beta1.Workload{}
	key := client.ObjectKey{Name: workloadName(js), Namespace: js.Namespace}
	if err := fakeClient.Get(context.Background(), key, workload); err != nil {
		t.Fatalf("getting created Workload: %v", err)
	}
	if got := workload.Labels[jobset.JobSetNameKey]; got != js.Name {
		t.Errorf("Workload label %q = %q, want %q", jobset.JobSetNameKey, got, js.Name)
	}
}

func TestReconcileSchedulingObjectsDeletesOwnedStalePodGroup(t *testing.T) {
	js := schedulingTestJobSet()
	scheme := schedulingTestScheme()
	workload, err := buildWorkload(js)
	if err != nil {
		t.Fatalf("buildWorkload() error = %v", err)
	}
	if err := ctrl.SetControllerReference(js, workload, scheme); err != nil {
		t.Fatalf("SetControllerReference(workload) error = %v", err)
	}

	builder := workloadbuilderForTest(workload, js)
	// Materialize the stale PodGroup with the same name the reconciler derives
	// from the (hashed) PodGroupTemplate name, so reconcileSchedulingObjects finds it.
	tmplName := workload.Spec.PodGroupTemplates[0].Name
	pg, err := builder.NewPodGroup(schedulingPodGroupName(js, tmplName), tmplName)
	if err != nil {
		t.Fatalf("NewPodGroup() error = %v", err)
	}
	pg.OwnerReferences = nil
	if err := ctrl.SetControllerReference(js, pg, scheme); err != nil {
		t.Fatalf("SetControllerReference(podgroup) error = %v", err)
	}
	// Simulate spec.scheduling having been switched from the default Gang policy
	// to Basic while the JobSet was suspended. The PodGroup's schedulingPolicy is
	// immutable upstream, so the stale object has to be deleted and recreated.
	// priorityClassName is deliberately not used as the drift signal here: the
	// API server substitutes a globalDefault PriorityClass for an empty one, so
	// podGroupSpecsEqualIgnoringMinCount cannot treat it as drift.
	pg.Spec.SchedulingPolicy = schedulingv1beta1.PodGroupSchedulingPolicy{
		Basic: &schedulingv1beta1.BasicSchedulingPolicy{},
	}
	pg.Labels = map[string]string{jobset.JobSetNameKey: js.Name}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(workload, pg).Build()
	r := &JobSetReconciler{Client: fakeClient, Scheme: scheme}
	if err := r.reconcileSchedulingObjects(context.Background(), js); err != nil {
		t.Fatalf("reconcileSchedulingObjects() error = %v", err)
	}

	// The stale objects must be deleted so the caller stops before creating Jobs
	// that reference the deleted PodGroup.
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(pg), &schedulingv1beta1.PodGroup{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale PodGroup still exists, get error = %v", err)
	}
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(workload), &schedulingv1beta1.Workload{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale Workload still exists, get error = %v", err)
	}
}

func TestReconcileSchedulingObjects(t *testing.T) {
	tests := map[string]struct {
		existing    bool
		mutate      func(*jobset.JobSet, *schedulingv1beta1.Workload, *schedulingv1beta1.PodGroup)
		wantDeleted bool
		wantError   string
	}{
		"creates Workload and PodGroup together": {},
		"matching objects are retained": {
			existing: true,
		},
		"scaling patches both minCounts in one reconcile": {
			existing: true,
			mutate: func(js *jobset.JobSet, _ *schedulingv1beta1.Workload, _ *schedulingv1beta1.PodGroup) {
				js.Spec.ReplicatedJobs[0].Replicas = 2
			},
		},
		"Workload drift stops before materializing PodGroups": {
			existing: true,
			mutate: func(_ *jobset.JobSet, workload *schedulingv1beta1.Workload, _ *schedulingv1beta1.PodGroup) {
				workload.Spec.PodGroupTemplates = nil
			},
			wantDeleted: true,
		},
		"rejects an unowned PodGroup": {
			existing: true,
			mutate: func(_ *jobset.JobSet, _ *schedulingv1beta1.Workload, pg *schedulingv1beta1.PodGroup) {
				pg.OwnerReferences = nil
			},
			wantError: "podgroup",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			js := schedulingTestJobSet()
			scheme := schedulingTestScheme()
			workload, err := buildWorkload(js)
			require.NoError(t, err)
			workload.OwnerReferences = nil
			require.NoError(t, ctrl.SetControllerReference(js, workload, scheme))
			tmplName := workload.Spec.PodGroupTemplates[0].Name
			pg, err := workloadbuilderForTest(workload, js).NewPodGroup(schedulingPodGroupName(js, tmplName), tmplName)
			require.NoError(t, err)
			pg.OwnerReferences = nil
			require.NoError(t, ctrl.SetControllerReference(js, pg, scheme))
			pg.Labels = map[string]string{jobset.JobSetNameKey: js.Name}
			if tc.mutate != nil {
				tc.mutate(js, workload, pg)
			}
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tc.existing {
				clientBuilder.WithObjects(workload, pg)
			}
			r := &JobSetReconciler{Client: clientBuilder.Build(), Scheme: scheme}

			err = r.reconcileSchedulingObjects(ctx, js)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				got := &schedulingv1beta1.PodGroup{}
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pg), got))
				assert.Equal(t, pg.Spec, got.Spec)
				assert.Empty(t, got.OwnerReferences)
				return
			}
			require.NoError(t, err)
			if tc.wantDeleted {
				assert.True(t, apierrors.IsNotFound(r.Get(ctx, client.ObjectKeyFromObject(workload), &schedulingv1beta1.Workload{})))
				assert.True(t, apierrors.IsNotFound(r.Get(ctx, client.ObjectKeyFromObject(pg), &schedulingv1beta1.PodGroup{})))
				// Replacement objects are only materialized on the next reconcile.
				require.NoError(t, r.reconcileSchedulingObjects(ctx, js))
			}

			desiredWorkload, err := buildWorkload(js)
			require.NoError(t, err)
			gotWorkload := &schedulingv1beta1.Workload{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desiredWorkload), gotWorkload))
			assert.Equal(t, desiredWorkload.Spec, gotWorkload.Spec)
			assert.True(t, metav1.IsControlledBy(gotWorkload, js))

			desiredPG, err := workloadbuilderForTest(desiredWorkload, js).NewPodGroup(schedulingPodGroupName(js, tmplName), tmplName)
			require.NoError(t, err)
			gotPG := &schedulingv1beta1.PodGroup{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desiredPG), gotPG))
			assert.Equal(t, desiredPG.Spec, gotPG.Spec)
			assert.Equal(t, js.Name, gotPG.Labels[jobset.JobSetNameKey])
			assert.True(t, metav1.IsControlledBy(gotPG, js))

			// Matching objects must not trigger deletion on subsequent reconciles.
			require.NoError(t, r.reconcileSchedulingObjects(ctx, js))
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desiredWorkload), &schedulingv1beta1.Workload{}))
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desiredPG), &schedulingv1beta1.PodGroup{}))
		})
	}
}

// Keep construction of the existing-workload builder in one place so tests
// exercise the same materialization path as the reconciler.
func workloadbuilderForTest(workload *schedulingv1beta1.Workload, js *jobset.JobSet) *workloadbuilder.Builder {
	return workloadbuilder.NewBuilderFromExistingWorkload(workload, buildOpts(js))
}

// scheduleObjectsForTest returns the owned Workload and PodGroup the reconciler
// would create for js, each carrying a finalizer so the fake client keeps them
// (in a terminating state) after a Delete instead of removing them outright.
func scheduleObjectsForTest(t *testing.T, js *jobset.JobSet, scheme *runtime.Scheme) (*schedulingv1beta1.Workload, *schedulingv1beta1.PodGroup) {
	t.Helper()
	workload, err := buildWorkload(js)
	require.NoError(t, err)
	workload.OwnerReferences = nil
	require.NoError(t, ctrl.SetControllerReference(js, workload, scheme))
	workload.Labels = map[string]string{jobset.JobSetNameKey: js.Name}
	workload.Finalizers = []string{"jobset.x-k8s.io/test-hold"}

	tmplName := workload.Spec.PodGroupTemplates[0].Name
	pg, err := workloadbuilderForTest(workload, js).NewPodGroup(schedulingPodGroupName(js, tmplName), tmplName)
	require.NoError(t, err)
	pg.OwnerReferences = nil
	require.NoError(t, ctrl.SetControllerReference(js, pg, scheme))
	pg.Labels = map[string]string{jobset.JobSetNameKey: js.Name}
	pg.Finalizers = []string{"jobset.x-k8s.io/test-hold"}
	return workload, pg
}

func TestSchedulingObjectsReady(t *testing.T) {
	tests := map[string]struct {
		// deleteWorkload / deletePodGroup leave the object terminating (held by a
		// finalizer) to mimic a suspend that has not finalized before resume.
		deleteWorkload bool
		deletePodGroup bool
		omitWorkload   bool
		omitPodGroup   bool
		wantReady      bool
	}{
		"Workload and PodGroup live": {wantReady: true},
		"Workload missing":           {omitWorkload: true, wantReady: false},
		"PodGroup missing":           {omitPodGroup: true, wantReady: false},
		"Workload terminating":       {deleteWorkload: true, wantReady: false},
		"PodGroup terminating":       {deletePodGroup: true, wantReady: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			js := schedulingTestJobSet()
			scheme := schedulingTestScheme()
			workload, pg := scheduleObjectsForTest(t, js, scheme)

			var objs []client.Object
			if !tc.omitWorkload {
				objs = append(objs, workload)
			}
			if !tc.omitPodGroup {
				objs = append(objs, pg)
			}
			r := &JobSetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(), Scheme: scheme}

			if tc.deleteWorkload {
				require.NoError(t, r.Delete(ctx, workload))
			}
			if tc.deletePodGroup {
				require.NoError(t, r.Delete(ctx, pg))
			}

			ready, err := r.schedulingObjectsReady(ctx, js)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReady, ready)
		})
	}
}

func TestReconcileSchedulingObjectsWaitsForTerminatingObjects(t *testing.T) {
	tests := map[string]struct {
		deleteWorkload bool
		deletePodGroup bool
	}{
		"terminating Workload": {deleteWorkload: true},
		"terminating PodGroup": {deletePodGroup: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			js := schedulingTestJobSet()
			scheme := schedulingTestScheme()
			workload, pg := scheduleObjectsForTest(t, js, scheme)
			r := &JobSetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(workload, pg).Build(), Scheme: scheme}

			if tc.deleteWorkload {
				require.NoError(t, r.Delete(ctx, workload))
			}
			if tc.deletePodGroup {
				require.NoError(t, r.Delete(ctx, pg))
			}

			// Reconcile must not error or patch a terminating object; it returns
			// early and leaves the gate (schedulingObjectsReady) to block Jobs.
			require.NoError(t, r.reconcileSchedulingObjects(ctx, js))
			ready, err := r.schedulingObjectsReady(ctx, js)
			require.NoError(t, err)
			assert.False(t, ready, "scheduling objects must not be ready while terminating")
		})
	}
}
