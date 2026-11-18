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

package conversion

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobsetv1 "sigs.k8s.io/jobset/api/jobset/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/util/testing"
)

// newJobSet returns a valid v1alpha2 JobSet with a leader and a workers
// ReplicatedJob.
func newJobSet(name, ns string) *testing.JobSetWrapper {
	rjob := func(name string) jobset.ReplicatedJob {
		return testing.MakeReplicatedJob(name).
			Job(testing.MakeJobTemplate("job", ns).PodSpec(testing.TestPodSpec).Obj()).
			Replicas(1).
			Obj()
	}
	return testing.MakeJobSet(name, ns).ReplicatedJob(rjob("leader")).ReplicatedJob(rjob("workers"))
}

var _ = Describe("JobSet conversion webhook", func() {
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "jobset-conversion-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("reads a v1alpha2 InOrder JobSet as v1 and keeps it InOrder on a v1 update", func() {
		js := newJobSet("in-order", ns.Name).
			StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
			FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 1, RestartStrategy: jobset.BlockingRecreate}).
			Obj()
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), got)).To(Succeed())
		Expect(got.Annotations).To(HaveKeyWithValue(jobsetv1.StartupPolicyAnnotationKey, string(jobset.InOrder)))
		Expect(got.Spec.ReplicatedJobs[0].DependsOn).To(BeEmpty())
		Expect(got.Spec.ReplicatedJobs[1].DependsOn).To(Equal([]jobsetv1.DependsOn{{Name: "leader", Status: jobsetv1.DependencyReady}}))
		Expect(got.Spec.FailurePolicy.RestartPolicy).To(Equal(jobsetv1.BlockingRecreate))

		By("updating the JobSet through v1")
		got.Labels = map[string]string{"updated": "true"}
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		stored := &jobset.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), stored)).To(Succeed())
		Expect(stored.Labels).To(HaveKeyWithValue("updated", "true"))
		Expect(stored.Spec.StartupPolicy).To(Equal(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}))
		Expect(stored.Annotations).NotTo(HaveKey(jobsetv1.StartupPolicyAnnotationKey))
		for _, rj := range stored.Spec.ReplicatedJobs {
			Expect(rj.DependsOn).To(BeEmpty())
		}
		Expect(stored.Spec.FailurePolicy.RestartStrategy).To(Equal(jobset.BlockingRecreate))
	})

	It("creates a JobSet through v1", func() {
		in := &jobsetv1.JobSet{}
		Expect(jobsetv1.Convert_v1alpha2_JobSet_To_v1_JobSet(newJobSet("from-v1", ns.Name).Obj(), in, nil)).To(Succeed())
		in.Spec.ReplicatedJobs[1].DependsOn = []jobsetv1.DependsOn{{Name: "leader", Status: jobsetv1.DependencyComplete}}
		Expect(k8sClient.Create(ctx, in)).To(Succeed())

		By("checking the stored v1alpha2 JobSet, defaulted by the v1alpha2 webhook")
		stored := &jobset.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), stored)).To(Succeed())
		Expect(stored.Spec.StartupPolicy).To(Equal(&jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}))
		Expect(stored.Spec.SuccessPolicy).NotTo(BeNil(), "the defaulting webhook did not run")
		Expect(stored.Spec.ReplicatedJobs[1].DependsOn).To(Equal([]jobset.DependsOn{{Name: "leader", Status: jobset.DependencyComplete}}))

		By("reading it back as v1")
		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), got)).To(Succeed())
		Expect(got.Annotations).NotTo(HaveKey(jobsetv1.StartupPolicyAnnotationKey))
		Expect(got.Spec.ReplicatedJobs[1].DependsOn).To(Equal(in.Spec.ReplicatedJobs[1].DependsOn))
	})

	It("reads the v1alpha2 status as v1", func() {
		js := newJobSet("status", ns.Name).Obj()
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		js.Status = jobset.JobSetStatus{
			TerminalState: string(jobset.JobSetCompleted),
			Conditions: []metav1.Condition{{
				Type:               string(jobset.JobSetCompleted),
				Status:             metav1.ConditionTrue,
				Reason:             "AllJobsCompleted",
				Message:            "jobset completed successfully",
				LastTransitionTime: metav1.Now(),
			}},
			ReplicatedJobsStatus: []jobset.ReplicatedJobStatus{{Name: "leader", Succeeded: 1}, {Name: "workers", Succeeded: 1}},
		}
		Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), got)).To(Succeed())
		Expect(got.Status.TerminalState).To(Equal(string(jobsetv1.JobSetComplete)))
		Expect(got.Status.Conditions).To(HaveLen(1))
		Expect(got.Status.Conditions[0].Type).To(Equal(string(jobsetv1.JobSetComplete)))
		Expect(got.Status.ReplicatedJobsStatus).To(HaveLen(2))
		Expect(got.Status.ReplicatedJobsStatus[0].Complete).To(Equal(int32(1)))
	})

	It("rejects a v1 JobSet without replicatedJobs", func() {
		in := &jobsetv1.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: ns.Name}}
		err := k8sClient.Create(ctx, in)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected an Invalid error, got %v", err)
		Expect(err.Error()).To(ContainSubstring("spec.replicatedJobs"))
	})
})
