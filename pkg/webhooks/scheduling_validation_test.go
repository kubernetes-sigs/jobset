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

func TestSchedulingWithSequencedStartup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	webhook := &jobSetWebhook{client: fake.NewFakeClient()}
	ctx := context.Background()

	for _, startup := range []string{"AnyOrder", "DependsOn", "InOrder", "empty DependsOn"} {
		for _, tc := range []struct {
			name       string
			scheduling *jobset.JobSetScheduling
			topGang    bool
		}{
			{name: "no scheduling"},
			{name: "default per-RJ gangs", scheduling: &jobset.JobSetScheduling{}},
			{
				name: "top-level Basic",
				scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{},
					},
				},
			},
			{
				name: "top-level Gang without minGroupCount", topGang: true,
				scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
				},
			},
			{
				name: "top-level Gang with minGroupCount", topGang: true,
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
				sequenced := startup == "DependsOn" || startup == "InOrder"
				if sequenced && tc.name == "default per-RJ gangs" {
					assert.Nil(t, js.Spec.Scheduling.SchedulingPolicy, "defaulting must not introduce a forbidden top-level Gang")
				}
				_, createErr := webhook.ValidateCreate(ctx, js)
				js.Spec.Suspend = ptr.To(true)
				_, updateErr := webhook.ValidateUpdate(ctx, old, js)
				for _, err := range []error{createErr, updateErr} {
					if sequenced && tc.topGang {
						require.Error(t, err)
						assert.True(t, apierrors.IsInvalid(err))
						assert.Contains(t, err.Error(), "spec.scheduling.schedulingPolicy.gang: Forbidden")
						assert.Contains(t, err.Error(), "use per-ReplicatedJob gang scheduling instead")
					} else {
						require.NoError(t, err)
					}
				}
			})
		}
	}
}
