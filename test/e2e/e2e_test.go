/*
Copyright 2023 The Kubernetes Authors.
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

package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("JobSet", func() {

	// Each test runs in a separate namespace.
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		// Create test namespace before each test.
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

		// Wait for namespace to exist before proceeding with test.
		gomega.Eventually(func() error {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
			if err != nil {
				return err
			}
			return nil
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		// Delete test namespace after each test.
		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("dns hostnames is enabled", func() {
		ginkgo.It("should enable pods to ping each other via hostname", func() {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset")
			js := pingTestJobSet(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(util.NumExpectedJobs(js)))

			// Check jobset status if specified.
			ginkgo.By("checking jobset condition")
			util.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})
	ginkgo.When("dns hostnames is enabled with custom subdomain", func() {
		ginkgo.It("should enable pods to ping each other via hostname with custom subdomain", func() {
			ctx := context.Background()

			// Create JobSet.
			ginkgo.By("creating jobset with subdomain")
			js := pingTestJobSetSubdomain(ns).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// We'll need to retry getting this newly created jobset, given that creation may not immediately happen.
			gomega.Eventually(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &jobset.JobSet{}), timeout, interval).Should(gomega.Succeed())

			ginkgo.By("checking all jobs were created successfully")
			gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(util.NumExpectedJobs(js)))

			// Check jobset status if specified.
			ginkgo.By("checking jobset condition")
			util.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})

}) // end of Describe

// getPingCommand returns ping command for 4 hostnames
// This bash script loops infinitely until it successfully pings all pods by hostname.
// Once successful, it sleeps for a short period to reduce flakiness, since occasionally
// all pods but one will successfully ping eachother and complete before the last one
// successfully pings them all, resulting in a failed test run.
func getPingCommand(hostnames []string) string {
	return fmt.Sprintf(`for pod in {"%s","%s","%s","%s"}
do
	gotStatus="-1"
	wantStatus="0"
	while [ $gotStatus -ne $wantStatus ]
	do                                       
		ping -c 1 $pod > /dev/null 2>&1
		gotStatus=$?                
		if [ $gotStatus -ne $wantStatus ]; then
			echo "Failed to ping pod $pod, retrying in 1 second..."
			sleep 1
		fi
	done                                                         
	echo "Successfully pinged pod: $pod"
done
sleep 30`, hostnames[0], hostnames[1], hostnames[2], hostnames[3])
}

// 1 replicated job with 4 replicas, DNS hostnames enabled
func pingTestJobSet(ns *corev1.Namespace) *testing.JobSetWrapper {
	jsName := "js"
	rjobName := "rjob"
	replicas := 4
	var podHostnames []string
	for jobIdx := 0; jobIdx < replicas; jobIdx++ {
		// Pod hostname format:
		// <jobSet.name>-<spec.replicatedJob.name>-<job-index>-<pod-index>.<jobSet.name>
		podHostnames = append(podHostnames, fmt.Sprintf("%s-%s-%d-0.%s", jsName, rjobName, jobIdx, jsName))
	}
	cmd := getPingCommand(podHostnames)
	return testing.MakeJobSet(jsName, ns.Name).
		EnableDNSHostnames(true).
		PublishNotReadyAddresses(true).
		ReplicatedJob(testing.MakeReplicatedJob(rjobName).
			Job(testing.MakeJobTemplate("job", ns.Name).
				PodSpec(corev1.PodSpec{
					RestartPolicy: "Never",
					Subdomain:     jsName,
					Containers: []corev1.Container{
						{
							Name:    "ping-test-container",
							Image:   "bash:latest",
							Command: []string{"bash", "-c"},
							Args:    []string{cmd},
						},
					},
				}).Obj()).
			Replicas(int32(replicas)).
			Obj())
}

// 1 replicated job with 4 replicas, DNS hostnames + subdomain enabled
func pingTestJobSetSubdomain(ns *corev1.Namespace) *testing.JobSetWrapper {
	jsName := "js"
	rjobName := "rjob"
	replicas := 4
	subdomain := "network-subdomain"
	var podHostnames []string
	for jobIdx := 0; jobIdx < replicas; jobIdx++ {
		// Pod hostname format:
		// e.g.,js-rjob-0-0.network-subdomain.e2e-7vd7z.svc.cluster.local       js-rjob-0-0
		// <jobSet.name>-<spec.replicatedJob.name>-<job-index>-<pod-index>.<subdomain>
		podHostnames = append(podHostnames, fmt.Sprintf("%s-%s-%d-0.%s", jsName, rjobName, jobIdx, subdomain))
	}
	cmd := getPingCommand(podHostnames)
	return testing.MakeJobSet(jsName, ns.Name).
		EnableDNSHostnames(true).
		PublishNotReadyAddresses(true).
		NetworkSubdomain(subdomain).
		ReplicatedJob(testing.MakeReplicatedJob(rjobName).
			Job(testing.MakeJobTemplate("job", ns.Name).
				PodSpec(corev1.PodSpec{
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:    "ping-test-container",
							Image:   "bash:latest",
							Command: []string{"bash", "-c"},
							Args:    []string{cmd},
						},
					},
				}).Obj()).
			Replicas(int32(replicas)).
			Obj())
}
