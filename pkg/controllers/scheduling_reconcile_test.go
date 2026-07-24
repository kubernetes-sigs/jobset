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

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
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
	utilruntime.Must(schedulingv1alpha3.AddToScheme(scheme))
	return scheme
}

func TestReconcileWorkloadRejectsUnownedExistingWorkload(t *testing.T) {
	js := schedulingTestJobSet()
	existing := &schedulingv1alpha3.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name: js.Name, Namespace: js.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: jobset.GroupVersion.String(), Kind: "JobSet", Name: js.Name, UID: "another-uid",
			}},
		},
	}
	scheme := schedulingTestScheme()
	r := &JobSetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(), Scheme: scheme}

	if _, err := r.reconcileWorkload(context.Background(), js); err == nil {
		t.Fatal("reconcileWorkload() returned nil for an unowned existing Workload")
	}
}

func TestReconcilePodGroupsDeletesOwnedStaleObjects(t *testing.T) {
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
	pg, err := builder.NewPodGroup(js.Name, js.Name)
	if err != nil {
		t.Fatalf("NewPodGroup() error = %v", err)
	}
	pg.OwnerReferences = nil
	if err := ctrl.SetControllerReference(js, pg, scheme); err != nil {
		t.Fatalf("SetControllerReference(podgroup) error = %v", err)
	}
	pg.Spec.PriorityClassName = "stale"

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(workload, pg).Build()
	r := &JobSetReconciler{Client: fakeClient, Scheme: scheme}
	if err := r.reconcilePodGroups(context.Background(), js); err != nil {
		t.Fatalf("reconcilePodGroups() error = %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(pg), &schedulingv1alpha3.PodGroup{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale PodGroup still exists, get error = %v", err)
	}
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(workload), &schedulingv1alpha3.Workload{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale Workload still exists, get error = %v", err)
	}
}

// Keep construction of the existing-workload builder in one place so tests
// exercise the same materialization path as the reconciler.
func workloadbuilderForTest(workload *schedulingv1alpha3.Workload, js *jobset.JobSet) *workloadbuilder.Builder {
	return workloadbuilder.NewBuilderFromExistingWorkload(workload, buildOpts(js))
}
