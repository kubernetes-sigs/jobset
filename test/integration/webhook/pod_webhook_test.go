// /*
// Copyright The Kubernetes Authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

// 	http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package webhooktest

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/test/util"
)

// The priority label is written by the Pod mutating webhook and then persisted by
// the API server, which validates label values. A negative PriorityClass value is
// legal in Kubernetes, so writing it verbatim produced a label the API server
// rejected and the Pod never got created. Exercising this through envtest rather
// than only in a unit test is what actually proves the Pod is admitted.
var _ = ginkgo.Describe("pod webhook priority label", func() {

	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: ns.Name}, ns)
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(util.DeleteNamespace(ctx, k8sClient, ns)).Should(gomega.Succeed())
	})

	// spec.priority cannot be set directly: the priority admission plugin rejects a
	// Pod that carries an integer priority and insists on deriving it from a
	// PriorityClass. So each case creates a PriorityClass and references it by name,
	// which is also how an operator actually hits this bug.
	makePriorityClass := func(name string, value int32) *schedulingv1.PriorityClass {
		return &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Value:      value,
		}
	}

	makePod := func(name, priorityClassName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns.Name,
				Annotations: map[string]string{
					jobset.JobSetNameKey: "js",
				},
			},
			Spec: corev1.PodSpec{
				PriorityClassName: priorityClassName,
				Containers: []corev1.Container{
					{
						Name:  "c",
						Image: "busybox:latest",
					},
				},
			},
		}
	}

	ginkgo.DescribeTable("admits the pod and records a valid priority label",
		func(podName, pcName string, priority int32, wantLabel string) {
			pc := makePriorityClass(pcName, priority)
			gomega.Expect(k8sClient.Create(ctx, pc)).To(gomega.Succeed())
			ginkgo.DeferCleanup(func() {
				gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pc))).To(gomega.Succeed())
			})

			pod := makePod(podName, pcName)

			// A label value the API server rejects fails here, so creation succeeding
			// is itself part of the assertion.
			gomega.Expect(k8sClient.Create(ctx, pod)).To(gomega.Succeed())

			created := &corev1.Pod{}
			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: podName}, created)
			}, timeout, interval).Should(gomega.Succeed())

			gomega.Expect(created.Spec.Priority).ToNot(gomega.BeNil())
			gomega.Expect(*created.Spec.Priority).To(gomega.Equal(priority))
			gomega.Expect(created.Labels).To(gomega.HaveKeyWithValue(constants.PriorityKey, wantLabel))
		},
		ginkgo.Entry("negative priority", "pod-negative", "pc-negative", int32(-1), "n1"),
		ginkgo.Entry("large negative priority", "pod-large-negative", "pc-large-negative", int32(-1000), "n1000"),
		ginkgo.Entry("zero priority", "pod-zero", "pc-zero", int32(0), "0"),
		ginkgo.Entry("positive priority", "pod-positive", "pc-positive", int32(100), "100"),
	)
})

var _ = ginkgo.Describe("pod webhook with filtered cache", func() {
	ginkgo.It("finds the scheduled leader when admitting an exclusive-placement follower", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "placement-cache-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(util.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		})

		const topologyKey = "topology.kubernetes.io/zone"
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "placement-cache-",
			Labels:       map[string]string{topologyKey: "test-zone"},
		}}
		gomega.Expect(k8sClient.Create(ctx, node)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() {
			gomega.Expect(k8sClient.Delete(ctx, node)).To(gomega.Succeed())
		})

		makePod := func(index string) *corev1.Pod {
			return &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-jobset-workers-0-" + index + "-",
					Namespace:    ns.Name,
					Labels: map[string]string{
						jobset.JobSetNameKey:        "test-jobset",
						jobset.ReplicatedJobNameKey: "workers",
						jobset.JobIndexKey:          "0",
						jobset.JobKey:               "test-job",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "test-jobset",
						jobset.ExclusiveKey:                  topologyKey,
						batchv1.JobCompletionIndexAnnotation: index,
					},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "batch/v1",
						Kind:       "Job",
						Name:       "test-job",
						UID:        "test-job-uid",
						Controller: ptr.To(true),
					}},
				},
				Spec: corev1.PodSpec{
					Containers:    []corev1.Container{{Name: "test", Image: "test-image"}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
		}

		leader := makePod("0")
		leader.Spec.NodeName = node.Name
		gomega.Expect(k8sClient.Create(ctx, leader)).To(gomega.Succeed())

		var follower *corev1.Pod
		gomega.Eventually(func() error {
			follower = makePod("1")
			return k8sClient.Create(ctx, follower)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Expect(follower.Spec.NodeSelector).To(gomega.HaveKeyWithValue(topologyKey, "test-zone"))
	})
})
