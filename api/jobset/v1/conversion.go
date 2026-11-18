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
	"fmt"
	"maps"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"

	"sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// This file holds the conversions between v1 and v1alpha2 that
// conversion-gen cannot generate. v1alpha2 is the hub (storage) version.
//
// The generated functions copy slices, maps and pointers without copying the
// data they point to, so the output shares memory with the input. The
// functions below clone anything they modify to keep the input unchanged.

// StartupPolicyAnnotationKey records the v1alpha2 startupPolicy of a JobSet
// that is read as v1. v1 has no startupPolicy: an InOrder startupPolicy is
// shown in v1 as a dependsOn chain, and this annotation lets the conversion
// back to v1alpha2 restore the original startupPolicy.
const StartupPolicyAnnotationKey = "jobset.sigs.k8s.io/v1alpha2-startup-policy"

// Convert_v1alpha2_JobSet_To_v1_JobSet converts a v1alpha2 JobSet to v1.
// An InOrder startupPolicy becomes a dependsOn chain, where every
// ReplicatedJob after the first depends on the previous one being Ready.
func Convert_v1alpha2_JobSet_To_v1_JobSet(in *v1alpha2.JobSet, out *JobSet, s conversion.Scope) error {
	if err := autoConvert_v1alpha2_JobSet_To_v1_JobSet(in, out, s); err != nil {
		return err
	}
	if in.Spec.StartupPolicy == nil || in.Spec.StartupPolicy.StartupPolicyOrder != v1alpha2.InOrder {
		return nil
	}

	replicatedJobs := slices.Clone(out.Spec.ReplicatedJobs)
	for i := range replicatedJobs {
		if len(replicatedJobs[i].DependsOn) > 0 {
			return fmt.Errorf("replicatedJob %q sets dependsOn together with the InOrder startupPolicy", replicatedJobs[i].Name)
		}
		if i > 0 {
			replicatedJobs[i].DependsOn = []DependsOn{{Name: replicatedJobs[i-1].Name, Status: DependencyReady}}
		}
	}
	out.Spec.ReplicatedJobs = replicatedJobs

	annotations := maps.Clone(out.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[StartupPolicyAnnotationKey] = string(v1alpha2.InOrder)
	out.Annotations = annotations
	return nil
}

// Convert_v1_JobSet_To_v1alpha2_JobSet converts a v1 JobSet to v1alpha2.
// A dependsOn chain produced from an InOrder startupPolicy, as recorded by
// StartupPolicyAnnotationKey, is turned back into that startupPolicy. Any other
// JobSet gets the default AnyOrder startupPolicy.
func Convert_v1_JobSet_To_v1alpha2_JobSet(in *JobSet, out *v1alpha2.JobSet, s conversion.Scope) error {
	if err := autoConvert_v1_JobSet_To_v1alpha2_JobSet(in, out, s); err != nil {
		return err
	}
	if !isInOrderChain(in) {
		out.Spec.StartupPolicy = &v1alpha2.StartupPolicy{StartupPolicyOrder: v1alpha2.AnyOrder}
		return nil
	}

	out.Spec.StartupPolicy = &v1alpha2.StartupPolicy{StartupPolicyOrder: v1alpha2.InOrder}
	replicatedJobs := slices.Clone(out.Spec.ReplicatedJobs)
	for i := range replicatedJobs {
		replicatedJobs[i].DependsOn = nil
	}
	out.Spec.ReplicatedJobs = replicatedJobs

	annotations := maps.Clone(out.Annotations)
	delete(annotations, StartupPolicyAnnotationKey)
	if len(annotations) == 0 {
		annotations = nil
	}
	out.Annotations = annotations
	return nil
}

// isInOrderChain reports whether js carries an InOrder startupPolicy from
// v1alpha2: the annotation is set, and dependsOn is exactly the chain that
// Convert_v1alpha2_JobSet_To_v1_JobSet produces.
func isInOrderChain(js *JobSet) bool {
	if js.Annotations[StartupPolicyAnnotationKey] != string(v1alpha2.InOrder) {
		return false
	}
	for i, rj := range js.Spec.ReplicatedJobs {
		if i == 0 {
			if len(rj.DependsOn) != 0 {
				return false
			}
			continue
		}
		want := DependsOn{Name: js.Spec.ReplicatedJobs[i-1].Name, Status: DependencyReady}
		if len(rj.DependsOn) != 1 || rj.DependsOn[0] != want {
			return false
		}
	}
	return true
}

// Convert_v1alpha2_JobSetSpec_To_v1_JobSetSpec drops startupPolicy, which has
// no v1 field. Convert_v1alpha2_JobSet_To_v1_JobSet maps it to dependsOn,
// since that also needs the JobSet annotations.
func Convert_v1alpha2_JobSetSpec_To_v1_JobSetSpec(in *v1alpha2.JobSetSpec, out *JobSetSpec, s conversion.Scope) error {
	return autoConvert_v1alpha2_JobSetSpec_To_v1_JobSetSpec(in, out, s)
}

// Convert_v1alpha2_JobSetStatus_To_v1_JobSetStatus renames the Completed
// condition type and terminal state to Complete.
func Convert_v1alpha2_JobSetStatus_To_v1_JobSetStatus(in *v1alpha2.JobSetStatus, out *JobSetStatus, s conversion.Scope) error {
	if err := autoConvert_v1alpha2_JobSetStatus_To_v1_JobSetStatus(in, out, s); err != nil {
		return err
	}
	from, to := string(v1alpha2.JobSetCompleted), string(JobSetComplete)
	out.Conditions = renameConditionType(out.Conditions, from, to)
	if out.TerminalState == from {
		out.TerminalState = to
	}
	return nil
}

// Convert_v1_JobSetStatus_To_v1alpha2_JobSetStatus renames the Complete
// condition type and terminal state back to Completed.
func Convert_v1_JobSetStatus_To_v1alpha2_JobSetStatus(in *JobSetStatus, out *v1alpha2.JobSetStatus, s conversion.Scope) error {
	if err := autoConvert_v1_JobSetStatus_To_v1alpha2_JobSetStatus(in, out, s); err != nil {
		return err
	}
	from, to := string(JobSetComplete), string(v1alpha2.JobSetCompleted)
	out.Conditions = renameConditionType(out.Conditions, from, to)
	if out.TerminalState == from {
		out.TerminalState = to
	}
	return nil
}

// renameConditionType returns conditions with the condition of type from
// renamed to to. It returns a copy when it renames, so the input is unchanged.
func renameConditionType(conditions []metav1.Condition, from, to string) []metav1.Condition {
	i := slices.IndexFunc(conditions, func(c metav1.Condition) bool { return c.Type == from })
	if i < 0 {
		return conditions
	}
	renamed := slices.Clone(conditions)
	renamed[i].Type = to
	return renamed
}

// Convert_v1alpha2_ReplicatedJobStatus_To_v1_ReplicatedJobStatus maps
// succeeded to complete.
func Convert_v1alpha2_ReplicatedJobStatus_To_v1_ReplicatedJobStatus(in *v1alpha2.ReplicatedJobStatus, out *ReplicatedJobStatus, s conversion.Scope) error {
	if err := autoConvert_v1alpha2_ReplicatedJobStatus_To_v1_ReplicatedJobStatus(in, out, s); err != nil {
		return err
	}
	out.Complete = in.Succeeded
	return nil
}

// Convert_v1_ReplicatedJobStatus_To_v1alpha2_ReplicatedJobStatus maps
// complete to succeeded.
func Convert_v1_ReplicatedJobStatus_To_v1alpha2_ReplicatedJobStatus(in *ReplicatedJobStatus, out *v1alpha2.ReplicatedJobStatus, s conversion.Scope) error {
	if err := autoConvert_v1_ReplicatedJobStatus_To_v1alpha2_ReplicatedJobStatus(in, out, s); err != nil {
		return err
	}
	out.Succeeded = in.Complete
	return nil
}

// Convert_v1alpha2_FailurePolicy_To_v1_FailurePolicy maps restartStrategy to
// restartPolicy.
func Convert_v1alpha2_FailurePolicy_To_v1_FailurePolicy(in *v1alpha2.FailurePolicy, out *FailurePolicy, s conversion.Scope) error {
	if err := autoConvert_v1alpha2_FailurePolicy_To_v1_FailurePolicy(in, out, s); err != nil {
		return err
	}
	out.RestartPolicy = JobSetRestartPolicy(in.RestartStrategy)
	return nil
}

// Convert_v1_FailurePolicy_To_v1alpha2_FailurePolicy maps restartPolicy to
// restartStrategy.
func Convert_v1_FailurePolicy_To_v1alpha2_FailurePolicy(in *FailurePolicy, out *v1alpha2.FailurePolicy, s conversion.Scope) error {
	if err := autoConvert_v1_FailurePolicy_To_v1alpha2_FailurePolicy(in, out, s); err != nil {
		return err
	}
	out.RestartStrategy = v1alpha2.JobSetRestartStrategy(in.RestartPolicy)
	return nil
}
