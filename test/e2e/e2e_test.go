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
	"os"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/printers"
	batchv1ac "k8s.io/client-go/applyconfigurations/batch/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetv1alpha2ac "sigs.k8s.io/jobset/client-go/applyconfiguration/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

func shouldDumpNamespace() bool {
	return os.Getenv("JOBSET_E2E_TESTS_DUMP_NAMESPACE") == "true"
}

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
		// Dump the namespace content, optionally.
		// This is only visible on failure since ginkgo.GinkgoWriter is being used.
		if shouldDumpNamespace() {
			var printer printers.YAMLPrinter

			fmt.Fprintf(ginkgo.GinkgoWriter, "\nDumping relevant resources in namespace %s:\n\n", ns.Name)

			// JobSets
			var jobsets jobset.JobSetList
			gomega.Expect(k8sClient.List(ctx, &jobsets)).To(gomega.Succeed())
			for _, js := range jobsets.Items {
				gomega.Expect(printer.PrintObj(&js, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}

			// Jobs
			var jobs batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobs)).To(gomega.Succeed())
			for _, job := range jobs.Items {
				// GVK is not set properly for the list items.
				job.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   batchv1.SchemeGroupVersion.Group,
					Version: batchv1.SchemeGroupVersion.Version,
					Kind:    "Job",
				})
				gomega.Expect(printer.PrintObj(&job, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}
		}

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
	ginkgo.When("ttl seconds after finished is set", func() {
		ginkgo.It("should clean up the completed jobset after configured ttl seconds expire", func() {
			ctx := context.Background()

			// Create JobSet.
			testFinalizer := "fake.example.com/blockDeletion"
			ginkgo.By("creating jobset with ttl seconds after finished")
			js := sleepTestJobSet(ns, 20).Finalizers([]string{testFinalizer}).TTLSecondsAfterFinished(5).Obj()

			// Verify jobset created successfully.
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// Check jobset status if specified.
			ginkgo.By("checking jobset condition")
			util.JobSetCompleted(ctx, k8sClient, js, timeout)

			// We remove the jobset finalizer, so it can get deleted when ttl expires.
			util.RemoveJobSetFinalizer(ctx, k8sClient, js, testFinalizer, timeout)

			// Check jobset is cleaned up after ttl seconds.
			ginkgo.By("checking jobset is cleaned up after ttl seconds")
		})
	})

	ginkgo.When("deleted using foreground propagation policy", func() {
		ginkgo.It("all child jobs should be deleted", func() {
			ctx := context.Background()

			// Create a JobSet.
			js := sleepTestJobSet(ns, 60).Obj()
			ginkgo.By("checking that jobset creation succeeds")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// Delete the JobSet using the foreground propagation policy.
			ginkgo.By("deleting the jobset using foreground propagation policy")
			gomega.Expect(k8sClient.Delete(ctx, js, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(gomega.Succeed())

			// Ensure the jobset is deleted.
			ginkgo.By("checking that jobset deletion succeeds")
			util.JobSetDeleted(ctx, k8sClient, js, timeout)

			// Ensure the child jobs are deleted.
			ginkgo.By("checking that all child jobs are deleted")
			gomega.Expect(util.NumJobs(ctx, k8sClient, js)).To(gomega.Equal(0))
		})
	})

	// This test is added to test the JobSet transitions as Kueue would when:
	// doing: resume in ResourceFlavor1 -> suspend -> resume in ResourceFlavor2.
	// In particular, Kueue updates the PodTemplate on suspending and resuming
	// the JobSet.
	ginkgo.When("JobSet is suspended and resumed", func() {

		ginkgo.It("should allow to resume JobSet after updating PodTemplate", func() {
			ctx := context.Background()
			js := sleepTestJobSet(ns, 1).Obj()
			jsKey := types.NamespacedName{Name: js.Name, Namespace: js.Namespace}

			ginkgo.By("Create a suspended JobSet", func() {
				js.Spec.Suspend = ptr.To(true)
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())
			})

			ginkgo.By("Unsuspend the JobSet setting nodeSelectors that prevent pods from being scheduled", func() {
				gomega.Eventually(func() error {
					gomega.Expect(k8sClient.Get(ctx, jsKey, js)).Should(gomega.Succeed())
					js.Spec.Suspend = ptr.To(false)
					podTemplate := &js.Spec.ReplicatedJobs[0].Template.Spec.Template
					if podTemplate.Spec.NodeSelector == nil {
						podTemplate.Spec.NodeSelector = make(map[string]string)
					}
					podTemplate.Spec.NodeSelector["kubernetes.io/os"] = "non-existing-os"
					return k8sClient.Update(ctx, js)
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("Await for all Jobs to be active", func() {
				// In this step the Pods remain Pending due to the nodeSelector
				// which does not match any nodes. Still, JobSet considers as
				// active any Jobs which have at least one Pending or Running Pod.
				gomega.Eventually(func() int32 {
					gomega.Expect(k8sClient.Get(ctx, jsKey, js)).Should(gomega.Succeed())
					if js.Status.ReplicatedJobsStatus == nil {
						return 0
					}
					return js.Status.ReplicatedJobsStatus[0].Active
				}, timeout, interval).Should(gomega.Equal(js.Spec.ReplicatedJobs[0].Replicas))
			})

			ginkgo.By("Suspend the JobSet updating the PodTemplate properties", func() {
				gomega.Eventually(func() error {
					gomega.Expect(k8sClient.Get(ctx, jsKey, js)).Should(gomega.Succeed())
					js.Spec.Suspend = ptr.To(true)
					podTemplate := &js.Spec.ReplicatedJobs[0].Template.Spec.Template
					podTemplate.Spec.NodeSelector["kubernetes.io/os"] = "linux"
					return k8sClient.Update(ctx, js)
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("Unsuspending the JobSet again with PodTemplate allowing completion", func() {
				gomega.Eventually(func() error {
					gomega.Expect(k8sClient.Get(ctx, jsKey, js)).Should(gomega.Succeed())
					js.Spec.Suspend = ptr.To(false)
					return k8sClient.Update(ctx, js)
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("Await for the JobSet to complete successfully", func() {
				util.JobSetCompleted(ctx, k8sClient, js, timeout)
			})
		})
	})

	// This test shows that when a JobSet is resumed it allows to add a
	// scheduling gate and propagates it down to Pods. This scenario is needed
	// for the integration with Kueue, to support TopologyAwareScheduling (TAS),
	// which adds the kueue.x-k8s.io/topology scheduling gate to control
	// assignment of Pods to the topology domains.
	ginkgo.When("JobSet is resumed is propagates scheduling gates to Pods", func() {

		ginkgo.It("should allow to add schedulingGates to PodTemplate while resuming", func() {
			ctx := context.Background()
			js := sleepTestJobSet(ns, 1).Obj()
			jsKey := types.NamespacedName{Name: js.Name, Namespace: js.Namespace}
			const (
				schedulingGateName = "example.com/gate"
			)

			ginkgo.By("Create a suspended JobSet", func() {
				js.Spec.Suspend = ptr.To(true)
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())
			})

			ginkgo.By("Resume the JobSet and set schedulingGates", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(k8sClient.Get(ctx, jsKey, js)).Should(gomega.Succeed())
					js.Spec.Suspend = ptr.To(false)
					podTemplate := &js.Spec.ReplicatedJobs[0].Template.Spec.Template
					podTemplate.Spec.SchedulingGates = append(podTemplate.Spec.SchedulingGates, corev1.PodSchedulingGate{
						Name: schedulingGateName,
					})
					g.Expect(k8sClient.Update(ctx, js)).Should(gomega.Succeed())
				}, timeout, interval).Should(gomega.Succeed())
			})

			// In this test the number of expected Pods equals the number of
			// expected Jobs as the Jobs don't set completions or parallelism,
			// so 1 Pod per Job is implied.
			expectedPods := util.NumExpectedJobs(js)
			ginkgo.By("Await for the expected number of gated pods created", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					list := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, list, client.InNamespace(js.Namespace))).Should(gomega.Succeed())
					gatedCount := 0
					for _, p := range list.Items {
						if len(p.Spec.SchedulingGates) == 1 && p.Spec.SchedulingGates[0].Name == schedulingGateName {
							gatedCount++
						}
					}
					g.Expect(gatedCount).Should(gomega.Equal(expectedPods),
						fmt.Sprintf("expected %v gated pods, got: %v, found items: %v", expectedPods, gatedCount, list.Items))
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("Ungate all of the pods to let the Job run and complete", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					list := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, list, client.InNamespace(js.Namespace))).Should(gomega.Succeed())
					for i := range list.Items {
						p := &list.Items[i]
						if len(p.Spec.SchedulingGates) == 1 && p.Spec.SchedulingGates[0].Name == schedulingGateName {
							p.Spec.SchedulingGates = nil
							g.Expect(k8sClient.Update(ctx, p)).Should(gomega.Succeed())
						}
					}
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("Await for the JobSet to complete successfully", func() {
				util.JobSetCompleted(ctx, k8sClient, js, timeout)
			})
		})
	})

	// This test runs JobSet with the DependsOn API.
	ginkgo.When("DependsOn is enabled on JobSet", func() {
		// This test shows that when a JobSet is used with the Kubeflow Trainer LLM Runtime.
		// The model-initializer and dataset-intializer Job should be completed before trainer node Job is started.
		ginkgo.It("trainer-node Job depends on dataset initializer and model initializer Jobs completion", func() {
			ctx := context.Background()

			numReplicas := 1
			modelInitializerJob := "model-initializer"
			datasetInitializerJob := "dataset-initializer"
			trainerJob := "trainer-node"

			// Every ReplicatedJob runs 1 container to sleep for 10 seconds.
			rJobModelInitializer := dependsOnTestReplicatedJob(ns, modelInitializerJob, numReplicas, nil, 10, nil)
			rJobDatasetInitializer := dependsOnTestReplicatedJob(ns, datasetInitializerJob, numReplicas, nil, 10, nil)
			rJobTrainer := dependsOnTestReplicatedJob(ns, trainerJob, numReplicas, nil, 0,
				[]jobset.DependsOn{
					{
						Name:   modelInitializerJob,
						Status: jobset.DependencyComplete,
					},
					{
						Name:   datasetInitializerJob,
						Status: jobset.DependencyComplete,
					},
				})

			jobSet := dependsOnTestJobSet(ns, []jobset.ReplicatedJob{rJobModelInitializer, rJobDatasetInitializer, rJobTrainer})
			jobSetKey := types.NamespacedName{Name: jobSet.Name, Namespace: jobSet.Namespace}

			ginkgo.By("Create a JobSet with DependsOn", func() {
				gomega.Expect(k8sClient.Create(ctx, jobSet)).Should(gomega.Succeed())
			})

			ginkgo.By("Wait for Model Initializer and Dataset Initializer to be in Ready/Succeeded status", func() {
				gomega.Eventually(util.NumJobsReadyOrSucceeded, timeout, interval).
					WithArguments(ctx, k8sClient, jobSet, modelInitializerJob).
					Should(gomega.Equal(int32(numReplicas)))
				gomega.Eventually(util.NumJobsReadyOrSucceeded, timeout, interval).
					WithArguments(ctx, k8sClient, jobSet, datasetInitializerJob).
					Should(gomega.Equal(int32(numReplicas)))
			})

			// We need to ensure that the E2E test reaches this check within 10 seconds of
			// the JobSet being created, as the Initializer has a 10-second sleep timer.
			// Otherwise, it will cause this check to fail.
			ginkgo.By("Verify that only Model Initializer and Dataset Initializer are created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(numReplicas * 2))
			})

			ginkgo.By("Wait for Model Initializer and Dataset Initializer to be in Completed status", func() {
				gomega.Expect(k8sClient.Get(ctx, jobSetKey, jobSet)).Should(gomega.Succeed())
				gomega.Eventually(func() int32 {
					gomega.Expect(k8sClient.Get(ctx, jobSetKey, jobSet)).Should(gomega.Succeed())
					for _, rJobStatus := range jobSet.Status.ReplicatedJobsStatus {
						if rJobStatus.Name == modelInitializerJob {
							return rJobStatus.Succeeded
						}
					}
					return 0
				}, timeout, interval).Should(gomega.Equal(int32(numReplicas)))
				gomega.Eventually(func() int32 {
					gomega.Expect(k8sClient.Get(ctx, jobSetKey, jobSet)).Should(gomega.Succeed())
					for _, rJobStatus := range jobSet.Status.ReplicatedJobsStatus {
						if rJobStatus.Name == datasetInitializerJob {
							return rJobStatus.Succeeded
						}
					}
					return 0
				}, timeout, interval).Should(gomega.Equal(int32(numReplicas)))
			})

			ginkgo.By("Verify that Model Initializer, Dataset Initializer, and Trainer Job is created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(util.NumExpectedJobs(jobSet)))
			})

			ginkgo.By("Wait for JobSet to be Completed", func() {
				util.JobSetCompleted(ctx, k8sClient, jobSet, timeout)
			})
		})
		// This test shows that when a JobSet is used for the Kubeflow Trainer with coordinator.
		// The coordinator Job should be ready before trainer node Job is started.
		ginkgo.It("trainer-node Job depends on coordinator Job ready status", func() {
			ctx := context.Background()

			numReplicas := 1
			coordinatorJob := "coordinator"
			trainerJob := "trainer-node"

			// Every ReplicatedJob runs 1 container to sleep for 10 seconds.
			rJobCoordinator := dependsOnTestReplicatedJob(ns, coordinatorJob, numReplicas,
				&corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"echo", "started"},
						},
					},
					InitialDelaySeconds: 5,
				},
				10, nil)

			rJobTrainer := dependsOnTestReplicatedJob(ns, trainerJob, numReplicas, nil, 0,
				[]jobset.DependsOn{
					{
						Name:   coordinatorJob,
						Status: jobset.DependencyReady,
					},
				})

			jobSet := dependsOnTestJobSet(ns, []jobset.ReplicatedJob{rJobCoordinator, rJobTrainer})

			ginkgo.By("Create a JobSet with DependsOn", func() {
				gomega.Expect(k8sClient.Create(ctx, jobSet)).Should(gomega.Succeed())
			})

			ginkgo.By("Verify that only Coordinator is created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(numReplicas))
			})

			// We need to ensure that the E2E test reaches this check within 10 seconds of
			// the JobSet being created, as the Coordinator has a 10-second sleep timer.
			// Otherwise, it will cause this check to fail.
			ginkgo.By("Wait for Coordinator Job to be in Ready/Succeeded status", func() {
				gomega.Eventually(util.NumJobsReadyOrSucceeded, timeout, interval).
					WithArguments(ctx, k8sClient, jobSet, coordinatorJob).
					Should(gomega.Equal(int32(numReplicas)))
			})

			ginkgo.By("Verify that Coordinator Job and Trainer Job is created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(util.NumExpectedJobs(jobSet)))
			})

			ginkgo.By("Wait for JobSet to be Completed", func() {
				util.JobSetCompleted(ctx, k8sClient, jobSet, timeout)
			})
		})
		// This test shows that when a JobSet is used with the Kubeflow Trainer MPI Runtime.
		// The launcher Job should be ready before trainer node Job is started.
		ginkgo.It("trainer-node Job depends on launcher Job ready status", func() {
			ctx := context.Background()

			numReplicasLauncher := 1
			numReplicasTrainer := 5
			launcherJob := "launcher"
			trainerJob := "trainer-node"

			// Launcher has startupProbe to sleep for 5 seconds.
			rJobLauncher := dependsOnTestReplicatedJob(ns, launcherJob, numReplicasLauncher,
				&corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"echo", "started"},
						},
					},
					InitialDelaySeconds: 5,
				},
				10, nil)

			rJobTrainer := dependsOnTestReplicatedJob(ns, trainerJob, numReplicasTrainer, nil, 0,
				[]jobset.DependsOn{
					{
						Name:   launcherJob,
						Status: jobset.DependencyReady,
					},
				})

			jobSet := dependsOnTestJobSet(ns, []jobset.ReplicatedJob{rJobLauncher, rJobTrainer})

			ginkgo.By("Create a JobSet with DependsOn", func() {
				gomega.Expect(k8sClient.Create(ctx, jobSet)).Should(gomega.Succeed())
			})

			// We need to ensure that the E2E test reaches this check within 10 seconds of
			// the JobSet being created, as the Launcher has a 10-second sleep timer.
			// Otherwise, it will cause this check to fail.
			ginkgo.By("Verify that only Launcher is created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(numReplicasLauncher))
			})

			ginkgo.By("Wait for Launcher to be in Ready/Succeeded status", func() {
				gomega.Eventually(util.NumJobsReadyOrSucceeded, timeout, interval).
					WithArguments(ctx, k8sClient, jobSet, launcherJob).
					Should(gomega.Equal(int32(numReplicasLauncher)))
			})

			// Launcher + Trainer has 6 replicas in total.
			ginkgo.By("Verify that Launcher and Trainer Job is created", func() {
				gomega.Eventually(util.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, jobSet).
					Should(gomega.Equal(util.NumExpectedJobs(jobSet)))
			})

			ginkgo.By("Wait for JobSet to be Completed", func() {
				util.JobSetCompleted(ctx, k8sClient, jobSet, timeout)
			})
		})
	})

	ginkgo.When("Using Server-Side Apply", func() {
		ginkgo.It("should not increment generation when re-applying the same JobSet apply configuration", func() {
			ctx := context.Background()

			// Create a JobSet apply configuration
			ginkgo.By("Creating JobSet using Server-Side Apply")
			jobSetConfig := serverSideApplyTestJobSet(ns, "test")

			// Convert the JobSet apply configuration into a client.Object
			u, err := apiruntime.DefaultUnstructuredConverter.ToUnstructured(jobSetConfig)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			patch := &unstructured.Unstructured{Object: u}

			// Create the JobSet using Server-Side Apply
			gomega.Expect(k8sClient.Patch(ctx, patch.DeepCopy(), client.Apply, client.ForceOwnership, fieldManagerName)).
				To(gomega.Succeed())

			// Wait for the JobSet to be created and get its initial generation
			ginkgo.By("Waiting for the JobSet to be created")
			jobSetKey := types.NamespacedName{Name: *jobSetConfig.GetName(), Namespace: ns.Name}
			jobSet := &jobset.JobSet{}
			gomega.Eventually(func() error {
				return k8sClient.Get(ctx, jobSetKey, jobSet)
			}, timeout, interval).Should(gomega.Succeed())

			generation := jobSet.Generation

			ginkgo.By("Re-applying the same JobSet apply configuration once")
			// Re-apply the original apply configuration
			gomega.Expect(k8sClient.Patch(ctx, patch.DeepCopy(), client.Apply, client.ForceOwnership, fieldManagerName)).
				To(gomega.Succeed())

			ginkgo.By("Re-applying the same JobSet apply configuration twice")
			// Re-apply the original apply configuration
			gomega.Expect(k8sClient.Patch(ctx, patch.DeepCopy(), client.Apply, client.ForceOwnership, fieldManagerName)).
				To(gomega.Succeed())

			// Get the JobSet again and verify generation hasn't changed
			ginkgo.By("Verifying the JobSet generation hasn't changed", func() {
				jobSetReApply := &jobset.JobSet{}
				gomega.Consistently(func() int64 {
					gomega.Expect(k8sClient.Get(ctx, jobSetKey, jobSetReApply)).To(gomega.Succeed())
					return jobSetReApply.Generation
				}, 10*time.Second, interval).Should(gomega.Equal(generation))
			})

			ginkgo.By("Cleaning up the JobSet")
			// Clean up by deleting the JobSet
			gomega.Expect(k8sClient.Delete(ctx, jobSet)).To(gomega.Succeed())
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

func sleepTestJobSet(ns *corev1.Namespace, durationSeconds int32) *testing.JobSetWrapper {
	jsName := "js"
	rjobName := "rjob"
	replicas := 4
	return testing.MakeJobSet(jsName, ns.Name).
		ReplicatedJob(testing.MakeReplicatedJob(rjobName).
			Job(testing.MakeJobTemplate("job", ns.Name).
				PodSpec(corev1.PodSpec{
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:    "sleep-test-container",
							Image:   "bash:latest",
							Command: []string{"bash", "-c"},
							Args:    []string{fmt.Sprintf("sleep %d", durationSeconds)},
						},
					},
				}).Obj()).
			Replicas(int32(replicas)).
			Obj())
}

func dependsOnTestJobSet(ns *corev1.Namespace, rJobs []jobset.ReplicatedJob) *jobset.JobSet {
	jobSet := testing.MakeJobSet("depends-on", ns.Name).Obj()
	jobSet.Spec.ReplicatedJobs = rJobs

	return jobSet
}

func dependsOnTestReplicatedJob(ns *corev1.Namespace, jobName string, numReplicas int, startupProbe *corev1.Probe, sleepSeconds int, dependsOn []jobset.DependsOn) jobset.ReplicatedJob {
	return testing.MakeReplicatedJob(jobName).
		Job(testing.MakeJobTemplate("job", ns.Name).
			PodSpec(corev1.PodSpec{
				RestartPolicy: "Never",
				Containers: []corev1.Container{
					{
						Name:         "sleep-test-container",
						Image:        "bash:latest",
						Command:      []string{"bash", "-c"},
						Args:         []string{"sleep " + strconv.Itoa(sleepSeconds)},
						StartupProbe: startupProbe,
					},
				},
			}).Obj()).
		Replicas(int32(numReplicas)).
		DependsOn(dependsOn).
		Obj()
}

// serverSideApplyTestJobSet creates a JobSet apply configuration for testing server-side apply
func serverSideApplyTestJobSet(ns *corev1.Namespace, name string) *jobsetv1alpha2ac.JobSetApplyConfiguration {
	return jobsetv1alpha2ac.JobSet(name, ns.Name).
		WithSpec(jobsetv1alpha2ac.JobSetSpec().
			WithReplicatedJobs(jobsetv1alpha2ac.ReplicatedJob().
				WithName("worker").
				WithReplicas(2).
				WithTemplate(batchv1ac.JobTemplateSpec().
					WithSpec(batchv1ac.JobSpec().
						WithTemplate(corev1ac.PodTemplateSpec().
							WithSpec(corev1ac.PodSpec().
								WithRestartPolicy(corev1.RestartPolicyNever).
								WithContainers(corev1ac.Container().
									WithName("test-container").
									WithImage("bash:latest").
									WithCommand("bash", "-c").
									WithArgs("echo 'Hello from server-side apply test'; sleep 5"),
								),
							),
						),
					),
				),
			),
		)
}
