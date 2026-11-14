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

package v1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/randfill"

	"sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// fuzzIterations is the number of random JobSets each round-trip test converts.
const fuzzIterations = 500

func hubJobSet(mutate func(*v1alpha2.JobSet)) *v1alpha2.JobSet {
	js := &v1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Annotations: map[string]string{"keep": "me"}},
		Spec: v1alpha2.JobSetSpec{
			ReplicatedJobs: []v1alpha2.ReplicatedJob{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			StartupPolicy:  &v1alpha2.StartupPolicy{StartupPolicyOrder: v1alpha2.AnyOrder},
		},
	}
	if mutate != nil {
		mutate(js)
	}
	return js
}

func spokeJobSet(mutate func(*JobSet)) *JobSet {
	js := &JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Annotations: map[string]string{"keep": "me"}},
		Spec: JobSetSpec{
			ReplicatedJobs: []ReplicatedJob{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		},
	}
	if mutate != nil {
		mutate(js)
	}
	return js
}

// inOrderChain sets the dependsOn chain that an InOrder startupPolicy maps to.
func inOrderChain(js *JobSet) {
	js.Annotations[StartupPolicyAnnotationKey] = string(v1alpha2.InOrder)
	js.Spec.ReplicatedJobs[1].DependsOn = []DependsOn{{Name: "a", Status: DependencyReady}}
	js.Spec.ReplicatedJobs[2].DependsOn = []DependsOn{{Name: "b", Status: DependencyReady}}
}

func TestConvertV1alpha2ToV1(t *testing.T) {
	cases := map[string]struct {
		in      *v1alpha2.JobSet
		want    *JobSet
		wantErr string
	}{
		"AnyOrder startupPolicy is dropped": {
			in:   hubJobSet(nil),
			want: spokeJobSet(nil),
		},
		"InOrder startupPolicy becomes a dependsOn chain": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.StartupPolicy.StartupPolicyOrder = v1alpha2.InOrder
			}),
			want: spokeJobSet(inOrderChain),
		},
		"InOrder startupPolicy without annotations": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Annotations = nil
				js.Spec.ReplicatedJobs = js.Spec.ReplicatedJobs[:1]
				js.Spec.StartupPolicy.StartupPolicyOrder = v1alpha2.InOrder
			}),
			want: spokeJobSet(func(js *JobSet) {
				js.Annotations = map[string]string{StartupPolicyAnnotationKey: string(v1alpha2.InOrder)}
				js.Spec.ReplicatedJobs = js.Spec.ReplicatedJobs[:1]
			}),
		},
		"InOrder startupPolicy together with dependsOn is rejected": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.StartupPolicy.StartupPolicyOrder = v1alpha2.InOrder
				js.Spec.ReplicatedJobs[1].DependsOn = []v1alpha2.DependsOn{{Name: "a", Status: v1alpha2.DependencyComplete}}
			}),
			wantErr: `replicatedJob "b" sets dependsOn together with the InOrder startupPolicy`,
		},
		"restartStrategy becomes restartPolicy": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.FailurePolicy = &v1alpha2.FailurePolicy{MaxRestarts: 2, RestartStrategy: v1alpha2.BlockingRecreate}
			}),
			want: spokeJobSet(func(js *JobSet) {
				js.Spec.FailurePolicy = &FailurePolicy{MaxRestarts: 2, RestartPolicy: BlockingRecreate}
			}),
		},
		"Completed status becomes Complete": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Status = v1alpha2.JobSetStatus{
					TerminalState: string(v1alpha2.JobSetCompleted),
					Conditions: []metav1.Condition{
						{Type: string(v1alpha2.JobSetStartupPolicyCompleted), Status: metav1.ConditionTrue},
						{Type: string(v1alpha2.JobSetCompleted), Status: metav1.ConditionTrue, Reason: "AllJobsCompleted"},
					},
					ReplicatedJobsStatus: []v1alpha2.ReplicatedJobStatus{{Name: "a", Ready: 1, Succeeded: 3, Failed: 2}},
				}
			}),
			want: spokeJobSet(func(js *JobSet) {
				js.Status = JobSetStatus{
					TerminalState: string(JobSetComplete),
					Conditions: []metav1.Condition{
						{Type: string(v1alpha2.JobSetStartupPolicyCompleted), Status: metav1.ConditionTrue},
						{Type: string(JobSetComplete), Status: metav1.ConditionTrue, Reason: "AllJobsCompleted"},
					},
					ReplicatedJobsStatus: []ReplicatedJobStatus{{Name: "a", Ready: 1, Complete: 3, Failed: 2}},
				}
			}),
		},
		"Failed status is unchanged": {
			in: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Status = v1alpha2.JobSetStatus{
					TerminalState: string(v1alpha2.JobSetFailed),
					Conditions:    []metav1.Condition{{Type: string(v1alpha2.JobSetFailed), Status: metav1.ConditionTrue}},
				}
			}),
			want: spokeJobSet(func(js *JobSet) {
				js.Status = JobSetStatus{
					TerminalState: string(JobSetFailed),
					Conditions:    []metav1.Condition{{Type: string(JobSetFailed), Status: metav1.ConditionTrue}},
				}
			}),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := tc.in.DeepCopy()
			got := &JobSet{}
			err := Convert_v1alpha2_JobSet_To_v1_JobSet(tc.in, got, nil)
			assert.Equal(t, before, tc.in, "conversion must not modify its input")
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestConvertV1ToV1alpha2(t *testing.T) {
	cases := map[string]struct {
		in   *JobSet
		want *v1alpha2.JobSet
	}{
		"no annotation gets the AnyOrder startupPolicy": {
			in:   spokeJobSet(nil),
			want: hubJobSet(nil),
		},
		"dependsOn without the annotation is kept": {
			in: spokeJobSet(func(js *JobSet) {
				js.Spec.ReplicatedJobs[1].DependsOn = []DependsOn{{Name: "a", Status: DependencyReady}}
			}),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.ReplicatedJobs[1].DependsOn = []v1alpha2.DependsOn{{Name: "a", Status: v1alpha2.DependencyReady}}
			}),
		},
		"InOrder dependsOn chain becomes the InOrder startupPolicy": {
			in: spokeJobSet(inOrderChain),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.StartupPolicy.StartupPolicyOrder = v1alpha2.InOrder
			}),
		},
		"annotation is removed entirely when it was the only one": {
			in: spokeJobSet(func(js *JobSet) {
				js.Annotations = map[string]string{StartupPolicyAnnotationKey: string(v1alpha2.InOrder)}
				js.Spec.ReplicatedJobs = js.Spec.ReplicatedJobs[:1]
			}),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Annotations = nil
				js.Spec.ReplicatedJobs = js.Spec.ReplicatedJobs[:1]
				js.Spec.StartupPolicy.StartupPolicyOrder = v1alpha2.InOrder
			}),
		},
		"annotation with a changed dependsOn chain is not InOrder": {
			in: spokeJobSet(func(js *JobSet) {
				inOrderChain(js)
				js.Spec.ReplicatedJobs[2].DependsOn[0].Status = DependencyComplete
			}),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Annotations[StartupPolicyAnnotationKey] = string(v1alpha2.InOrder)
				js.Spec.ReplicatedJobs[1].DependsOn = []v1alpha2.DependsOn{{Name: "a", Status: v1alpha2.DependencyReady}}
				js.Spec.ReplicatedJobs[2].DependsOn = []v1alpha2.DependsOn{{Name: "b", Status: v1alpha2.DependencyComplete}}
			}),
		},
		"restartPolicy becomes restartStrategy": {
			in: spokeJobSet(func(js *JobSet) {
				js.Spec.FailurePolicy = &FailurePolicy{RestartPolicy: InPlaceRestart}
			}),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Spec.FailurePolicy = &v1alpha2.FailurePolicy{RestartStrategy: v1alpha2.InPlaceRestart}
			}),
		},
		"Complete status becomes Completed": {
			in: spokeJobSet(func(js *JobSet) {
				js.Status = JobSetStatus{
					TerminalState:        string(JobSetComplete),
					Conditions:           []metav1.Condition{{Type: string(JobSetComplete), Status: metav1.ConditionTrue}},
					ReplicatedJobsStatus: []ReplicatedJobStatus{{Name: "a", Complete: 4}},
				}
			}),
			want: hubJobSet(func(js *v1alpha2.JobSet) {
				js.Status = v1alpha2.JobSetStatus{
					TerminalState:        string(v1alpha2.JobSetCompleted),
					Conditions:           []metav1.Condition{{Type: string(v1alpha2.JobSetCompleted), Status: metav1.ConditionTrue}},
					ReplicatedJobsStatus: []v1alpha2.ReplicatedJobStatus{{Name: "a", Succeeded: 4}},
				}
			}),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := tc.in.DeepCopy()
			got := &v1alpha2.JobSet{}
			require.NoError(t, Convert_v1_JobSet_To_v1alpha2_JobSet(tc.in, got, nil))
			assert.Equal(t, before, tc.in, "conversion must not modify its input")
			assert.Equal(t, tc.want, got)
		})
	}
}

// newFiller returns a filler for random JobSets. TypeMeta is left empty, since
// the caller (the conversion webhook) sets it on the converted object. Pod
// templates are left empty too: the conversion copies them as they are, and
// filling them only slows the test.
func newFiller(seed int64) *randfill.Filler {
	return randfill.NewWithSeed(seed).NilChance(0.3).NumElements(0, 3).Funcs(
		func(*metav1.TypeMeta, randfill.Continue) {},
		func(t *batchv1.JobTemplateSpec, c randfill.Continue) {
			*t = batchv1.JobTemplateSpec{}
			c.Fill(&t.Labels)
		},
		func(sp *v1alpha2.StartupPolicy, c randfill.Continue) {
			sp.StartupPolicyOrder = v1alpha2.AnyOrder
			if c.Bool() {
				sp.StartupPolicyOrder = v1alpha2.InOrder
			}
		},
	)
}

// TestRoundTripV1alpha2 converts random v1alpha2 JobSets to v1 and back.
// The only expected difference is that a missing startupPolicy comes back as
// the default AnyOrder.
func TestRoundTripV1alpha2(t *testing.T) {
	f := newFiller(1)
	for i := range fuzzIterations {
		in := &v1alpha2.JobSet{}
		f.Fill(in)
		if in.Spec.StartupPolicy != nil && in.Spec.StartupPolicy.StartupPolicyOrder == v1alpha2.InOrder {
			// The API rejects InOrder together with dependsOn.
			for j := range in.Spec.ReplicatedJobs {
				in.Spec.ReplicatedJobs[j].DependsOn = nil
			}
		}
		want := in.DeepCopy()
		if want.Spec.StartupPolicy == nil {
			want.Spec.StartupPolicy = &v1alpha2.StartupPolicy{StartupPolicyOrder: v1alpha2.AnyOrder}
		}

		spoke := &JobSet{}
		require.NoError(t, Convert_v1alpha2_JobSet_To_v1_JobSet(in, spoke, nil), "iteration %d", i)
		got := &v1alpha2.JobSet{}
		require.NoError(t, Convert_v1_JobSet_To_v1alpha2_JobSet(spoke, got, nil), "iteration %d", i)

		if !apiequality.Semantic.DeepEqual(want, got) {
			t.Fatalf("iteration %d: round trip changed the JobSet (-want +got):\n%s", i, cmp.Diff(want, got))
		}
	}
}

// TestRoundTripV1 converts random v1 JobSets to v1alpha2 and back, and
// expects them unchanged.
func TestRoundTripV1(t *testing.T) {
	f := newFiller(2)
	for i := range fuzzIterations {
		in := &JobSet{}
		f.Fill(in)
		if i%2 == 0 && len(in.Spec.ReplicatedJobs) > 0 {
			// Half of the JobSets carry an InOrder chain, as produced from v1alpha2.
			if in.Annotations == nil {
				in.Annotations = map[string]string{}
			}
			in.Annotations[StartupPolicyAnnotationKey] = string(v1alpha2.InOrder)
			for j := range in.Spec.ReplicatedJobs {
				in.Spec.ReplicatedJobs[j].DependsOn = nil
				if j > 0 {
					in.Spec.ReplicatedJobs[j].DependsOn = []DependsOn{{Name: in.Spec.ReplicatedJobs[j-1].Name, Status: DependencyReady}}
				}
			}
		}
		want := in.DeepCopy()

		hub := &v1alpha2.JobSet{}
		require.NoError(t, Convert_v1_JobSet_To_v1alpha2_JobSet(in, hub, nil), "iteration %d", i)
		got := &JobSet{}
		require.NoError(t, Convert_v1alpha2_JobSet_To_v1_JobSet(hub, got, nil), "iteration %d", i)

		if !apiequality.Semantic.DeepEqual(want, got) {
			t.Fatalf("iteration %d: round trip changed the JobSet (-want +got):\n%s", i, cmp.Diff(want, got))
		}
	}
}
