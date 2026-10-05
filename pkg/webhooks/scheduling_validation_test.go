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

package webhooks

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
)

func TestSchedulingRejectsTooManyPodGroupTemplates(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	webhook := &jobSetWebhook{client: fake.NewFakeClient()}
	ctx := context.Background()

	// job: {} compiles one PodGroupTemplate per replica. The upstream Workload
	// API caps a Workload at 8 templates, so replicas > 8 must be rejected at
	// admission rather than failing Workload creation as Invalid on every
	// reconcile (which would create no Jobs).
	for _, tc := range []struct {
		name     string
		replicas int32
		wantErr  bool
	}{
		{name: "at the limit", replicas: 8},
		{name: "over the limit", replicas: 16, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			js := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "too-many"}}
			js.Spec.ReplicatedJobs = []jobset.ReplicatedJob{{
				Name: "workers", GroupName: "default", Replicas: tc.replicas,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1), Template: *TestPodTemplate.DeepCopy()}},
			}}
			js.Spec.Scheduling = &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"workers"}, Job: &jobset.JobScheduling{}},
				},
			}
			require.NoError(t, webhook.Default(ctx, js))

			_, err := webhook.ValidateCreate(ctx, js)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, apierrors.IsInvalid(err))
			assert.Contains(t, err.Error(), "spec.scheduling: Too many: 16: must have at most 8 items")
		})
	}
}

func TestSchedulingRejectsTotalPodCountOverflow(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	webhook := &jobSetWebhook{client: fake.NewFakeClient()}
	ctx := context.Background()

	// A top-level gang PodGroup's minCount is the sum of every ReplicatedJob's
	// pod count. Each per-RJ product is <= MaxInt32 (so the per-RJ check passes),
	// but their sum overflows int32 and must be rejected rather than saturated.
	makeJS := func(name string, perRJParallelism int32, rjCount int) *jobset.JobSet {
		js := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: name}}
		for i := 0; i < rjCount; i++ {
			js.Spec.ReplicatedJobs = append(js.Spec.ReplicatedJobs, jobset.ReplicatedJob{
				Name: fmt.Sprintf("rj-%d", i), GroupName: "default", Replicas: 1,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To(perRJParallelism), Template: *TestPodTemplate.DeepCopy()}},
			})
		}
		js.Spec.Scheduling = &jobset.JobSetScheduling{} // defaulted top-level gang
		return js
	}

	t.Run("total within int32 is accepted", func(t *testing.T) {
		js := makeJS("total-ok", 1000, 2)
		require.NoError(t, webhook.Default(ctx, js))
		_, err := webhook.ValidateCreate(ctx, js)
		require.NoError(t, err)
	})

	t.Run("total exceeding int32 is rejected", func(t *testing.T) {
		js := makeJS("total-overflow", math.MaxInt32, 2)
		require.NoError(t, webhook.Default(ctx, js))
		_, err := webhook.ValidateCreate(ctx, js)
		require.Error(t, err)
		assert.True(t, apierrors.IsInvalid(err))
		assert.Contains(t, err.Error(), "total represented pod count across all replicatedJobs must not exceed")
	})
}

func TestSchedulingWithSequencedStartup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	webhook := &jobSetWebhook{client: fake.NewFakeClient()}
	ctx := context.Background()

	for _, startup := range []string{"AnyOrder", "DependsOn", "InOrder", "empty DependsOn"} {
		for _, tc := range []struct {
			name       string
			scheduling *jobset.JobSetScheduling
		}{
			{name: "no scheduling"},
			{name: "default top-level gang", scheduling: &jobset.JobSetScheduling{}},
			{
				name: "top-level Basic",
				scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{},
					},
				},
			},
			{
				name: "top-level Gang without minGroupCount",
				scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
				},
			},
			{
				name: "top-level Gang with minGroupCount",
				scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{MinGroupCount: ptr.To[int32](1)},
					},
				},
			},
			{
				name: "explicit per-RJ gangs",
				scheduling: &jobset.JobSetScheduling{
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"driver"}, SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}},
						{TargetReplicatedJobs: []string{"workers"}, SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}},
					},
				},
			},
		} {
			t.Run(startup+"/"+tc.name, func(t *testing.T) {
				js := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "sequenced"}}
				for _, name := range []string{"driver", "workers"} {
					js.Spec.ReplicatedJobs = append(js.Spec.ReplicatedJobs, jobset.ReplicatedJob{
						Name: name, GroupName: "default", Replicas: 1,
						Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1), Template: *TestPodTemplate.DeepCopy()}},
					})
				}
				switch startup {
				case "DependsOn":
					js.Spec.ReplicatedJobs[1].DependsOn = []jobset.DependsOn{{Name: "driver", Status: jobset.DependencyReady}}
				case "InOrder":
					js.Spec.StartupPolicy = &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}
				case "empty DependsOn":
					js.Spec.ReplicatedJobs[1].DependsOn = []jobset.DependsOn{}
				}
				require.NoError(t, webhook.Default(ctx, js))
				old := js.DeepCopy()
				old.Spec.Suspend = ptr.To(true)
				old.Status.Conditions = []metav1.Condition{{Type: string(jobset.JobSetSuspended), Status: metav1.ConditionTrue}}

				js.Spec.Scheduling = tc.scheduling.DeepCopy()
				require.NoError(t, webhook.Default(ctx, js))
				if tc.name == "default top-level gang" {
					// Empty spec.scheduling defaults to a top-level gang policy
					// regardless of startup order; the CEL rule (not defaulting)
					// then rejects the combination with sequenced startup.
					require.NotNil(t, js.Spec.Scheduling.SchedulingPolicy)
					assert.NotNil(t, js.Spec.Scheduling.SchedulingPolicy.Gang, "empty scheduling must default to a top-level gang policy")
				}
				_, createErr := webhook.ValidateCreate(ctx, js)
				js.Spec.Suspend = ptr.To(true)
				_, updateErr := webhook.ValidateUpdate(ctx, old, js)
				// The webhook must accept all of these configurations. Rejecting
				// top-level scheduling combined with sequenced startup is enforced by
				// CEL on the CRD (see the scheduling integration tests), not here, so
				// the webhook must not re-introduce that duplicate validation.
				for _, err := range []error{createErr, updateErr} {
					require.NoError(t, err)
				}
			})
		}
	}
}
