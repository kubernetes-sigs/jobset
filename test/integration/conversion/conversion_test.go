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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobsetv1 "sigs.k8s.io/jobset/api/jobset/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/util/testing"
)

// externalManager is the managedBy of the JobSets in these tests, so the
// JobSet controller leaves them alone. Only the spec that checks the
// controller clears it.
const externalManager = "example.com/external"

// newJobSet returns a valid v1alpha2 JobSet with a leader and a workers
// ReplicatedJob, managed by externalManager.
func newJobSet(name, ns string) *testing.JobSetWrapper {
	rjob := func(name string) jobset.ReplicatedJob {
		return testing.MakeReplicatedJob(name).
			Job(testing.MakeJobTemplate("job", ns).PodSpec(testing.TestPodSpec).Obj()).
			Replicas(1).
			Obj()
	}
	return testing.MakeJobSet(name, ns).
		ManagedBy(externalManager).
		ReplicatedJob(rjob("leader")).
		ReplicatedJob(rjob("workers"))
}

// newV1JobSet returns newJobSet converted to v1.
func newV1JobSet(name, ns string) *jobsetv1.JobSet {
	js := &jobsetv1.JobSet{}
	Expect(jobsetv1.Convert_v1alpha2_JobSet_To_v1_JobSet(newJobSet(name, ns).Obj(), js, nil)).To(Succeed())
	return js
}

// completeJob sets the status the Job controller sets on a Job whose pods all
// succeeded. envtest runs no Job controller.
func completeJob(job *batchv1.Job) {
	now := metav1.Now()
	job.Status = batchv1.JobStatus{
		StartTime:      &now,
		CompletionTime: &now,
		Succeeded:      ptr.Deref(job.Spec.Parallelism, 0),
		Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		},
	}
	Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
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

	It("rejects a v1 update of an InOrder JobSet that drops the annotation", func() {
		// Known limitation: the conversion cannot see the stored object, so
		// without the annotation the JobSet converts to AnyOrder plus a dependsOn
		// chain, which the immutability rules reject.
		js := newJobSet("drop-annotation", ns.Name).
			StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
			Obj()
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), got)).To(Succeed())
		delete(got.Annotations, jobsetv1.StartupPolicyAnnotationKey)
		err := k8sClient.Update(ctx, got)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected an Invalid error, got %v", err)
		Expect(err.Error()).To(ContainSubstring("immutable"))

		stored := &jobset.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), stored)).To(Succeed())
		Expect(stored.Spec.StartupPolicy).To(Equal(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}))
	})

	It("does not store a stray annotation on a v1 create", func() {
		in := newV1JobSet("stray-annotation", ns.Name)
		in.Annotations = map[string]string{jobsetv1.StartupPolicyAnnotationKey: string(jobset.InOrder)}
		Expect(k8sClient.Create(ctx, in)).To(Succeed())

		stored := &jobset.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), stored)).To(Succeed())
		Expect(stored.Spec.StartupPolicy).To(Equal(&jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}))
		Expect(stored.Annotations).NotTo(HaveKey(jobsetv1.StartupPolicyAnnotationKey))

		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), got)).To(Succeed())
		Expect(got.Annotations).NotTo(HaveKey(jobsetv1.StartupPolicyAnnotationKey))
	})

	It("keeps a v1 Ready chain without the annotation as dependsOn", func() {
		in := newV1JobSet("ready-chain", ns.Name)
		chain := []jobsetv1.DependsOn{{Name: "leader", Status: jobsetv1.DependencyReady}}
		in.Spec.ReplicatedJobs[1].DependsOn = chain
		Expect(k8sClient.Create(ctx, in)).To(Succeed())

		stored := &jobset.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), stored)).To(Succeed())
		Expect(stored.Spec.StartupPolicy).To(Equal(&jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}))
		Expect(stored.Spec.ReplicatedJobs[1].DependsOn).To(Equal([]jobset.DependsOn{{Name: "leader", Status: jobset.DependencyReady}}))

		got := &jobsetv1.JobSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), got)).To(Succeed())
		Expect(got.Annotations).NotTo(HaveKey(jobsetv1.StartupPolicyAnnotationKey))
		Expect(got.Spec.ReplicatedJobs[1].DependsOn).To(Equal(chain))
	})

	It("reconciles a JobSet created through v1 and reports its status as v1", func() {
		in := newV1JobSet("reconciled", ns.Name)
		in.Spec.ManagedBy = nil
		Expect(k8sClient.Create(ctx, in)).To(Succeed())

		By("waiting for the controller to create the jobs")
		jobs := &batchv1.JobList{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.List(ctx, jobs, client.InNamespace(ns.Name), client.MatchingLabels{jobset.JobSetNameKey: in.Name})).To(Succeed())
			g.Expect(jobs.Items).To(HaveLen(2))
		}).Should(Succeed())

		By("completing the jobs")
		for i := range jobs.Items {
			completeJob(&jobs.Items[i])
		}

		By("checking the status read through v1")
		Eventually(func(g Gomega) {
			got := &jobsetv1.JobSet{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(in), got)).To(Succeed())
			g.Expect(got.Status.TerminalState).To(Equal(string(jobsetv1.JobSetComplete)))
			g.Expect(got.Status.Conditions).To(ContainElement(HaveField("Type", string(jobsetv1.JobSetComplete))))
			g.Expect(got.Status.ReplicatedJobsStatus).To(ConsistOf(
				HaveField("Complete", int32(1)),
				HaveField("Complete", int32(1)),
			))
		}).Should(Succeed())
	})

	It("creates a JobSet through v1", func() {
		in := newV1JobSet("from-v1", ns.Name)
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
