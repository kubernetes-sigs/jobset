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

package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(jobset.AddToScheme(scheme))

	inPlaceRestartExitCode := 123

	testCases := []struct {
		name                          string
		workerCommand                 string // Only used in the tests to determine if the agent is in sidecar container mode or in main container mode
		currentInPlaceRestartAttempt  *int32
		previousInPlaceRestartAttempt *int32
		podInPlaceRestartAttempt      *int32
		isBarrierActive               bool
		jobSetAnnotations             map[string]string
		restartStrategy               jobset.JobSetRestartStrategy
		wantPodAnnotation             *string
		wantExitCode                  *int
		wantWorkerExecuted            bool
		wantStartupProbeServerStarted bool
	}{
		{
			name:                          "Agent starts for the first time ever (because the JobSet just got created)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  nil,
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             ptr.To("0"),
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent starts for the second time (because it is the source of failure in the first JobSet restart)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             ptr.To("1"),
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent starts for the third time (because it is the source of failure in the second JobSet restart)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](1),
			previousInPlaceRestartAttempt: ptr.To[int32](0),
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             ptr.To("2"),
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent starts for the second time (because it was forced to restart as part of the first JobSet restart)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: ptr.To[int32](0),
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             ptr.To("1"),
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent exits to restart its Pod in-place (because it was forced to restart as part of the first JobSet restart)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: ptr.To[int32](0),
			podInPlaceRestartAttempt:      ptr.To[int32](0),
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  ptr.To(inPlaceRestartExitCode),
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent lifts its barrier and executes its worker for the first time (because all agents are in sync)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      ptr.To[int32](0),
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            true,
		},
		{
			name:                          "Agent lifts its barrier and executes its worker for the second time (because all agents are in sync)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](1),
			previousInPlaceRestartAttempt: ptr.To[int32](0),
			podInPlaceRestartAttempt:      ptr.To[int32](1),
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            true,
		},
		{
			name:                          "Agent does nothing (0 restarts so far)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      ptr.To[int32](0),
			isBarrierActive:               false,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent does nothing (1 restart so far)",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  ptr.To[int32](1),
			previousInPlaceRestartAttempt: ptr.To[int32](0),
			podInPlaceRestartAttempt:      ptr.To[int32](1),
			isBarrierActive:               false,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
		},
		{
			name:                          "Agent bypasses barrier because DisableInPlaceRestartKey annotation is present",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  nil,
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			jobSetAnnotations: map[string]string{
				"jobset.sigs.k8s.io/disable-in-place-restart": "",
			},
			restartStrategy:    "InPlaceRestart",
			wantPodAnnotation:  nil,
			wantExitCode:       nil,
			wantWorkerExecuted: true,
		},
		{
			name:                          "Agent bypasses barrier because RestartStrategy is not InPlaceRestart",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  nil,
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "Recreate",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            true,
		},
		{
			name:                          "Agent bypasses barrier because RestartStrategy is explicitly empty",
			workerCommand:                 "echo hello",
			currentInPlaceRestartAttempt:  nil,
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            true,
		},
		{
			name:                          "Agent lifts its barrier and starts startup probe server in sidecar container mode (because all agents are in sync)",
			workerCommand:                 "",
			currentInPlaceRestartAttempt:  ptr.To[int32](0),
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      ptr.To[int32](0),
			isBarrierActive:               true,
			restartStrategy:               "InPlaceRestart",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
			wantStartupProbeServerStarted: true,
		},
		{
			name:                          "Agent bypasses barrier in sidecar container mode because RestartStrategy is not InPlaceRestart",
			workerCommand:                 "",
			currentInPlaceRestartAttempt:  nil,
			previousInPlaceRestartAttempt: nil,
			podInPlaceRestartAttempt:      nil,
			isBarrierActive:               true,
			restartStrategy:               "Recreate",
			wantPodAnnotation:             nil,
			wantExitCode:                  nil,
			wantWorkerExecuted:            false,
			wantStartupProbeServerStarted: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup objects
			js := &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-jobset",
					Namespace:   "default",
					Annotations: tc.jobSetAnnotations,
				},
				Spec: jobset.JobSetSpec{
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: tc.restartStrategy,
					},
				},
				Status: jobset.JobSetStatus{
					CurrentInPlaceRestartAttempt:  tc.currentInPlaceRestartAttempt,
					PreviousInPlaceRestartAttempt: tc.previousInPlaceRestartAttempt,
				},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			// Setup fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(js, pod).
				WithStatusSubresource(js).
				Build()

			// Setup agent
			exitCodeChannel := make(chan int, 1)
			workerExecutedChannel := make(chan bool, 1)
			startupProbeServerStartedChannel := make(chan bool, 1)
			agent := &InPlaceRestartAgent{
				Client:                   fakeClient,
				Namespace:                "default",
				PodName:                  "test-pod",
				WorkerCommand:            tc.workerCommand,
				StartupProbePath:         "/barrier-is-down",
				StartupProbePort:         "8081",
				InPlaceRestartExitCode:   inPlaceRestartExitCode,
				PodInPlaceRestartAttempt: tc.podInPlaceRestartAttempt,
				IsBarrierActive:          tc.isBarrierActive,
				Exit: func(code int) {
					exitCodeChannel <- code
				},
				StartWorker: func(ctx context.Context, command string) error {
					workerExecutedChannel <- true
					return nil
				},
				StartStartupProbe: func(addr string, handler http.Handler) error {
					startupProbeServerStartedChannel <- true
					return nil
				},
			}

			// Reconcile
			req := ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(js),
			}
			_, err := agent.Reconcile(context.Background(), req)
			if err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}

			// Verify pod annotation
			if tc.wantPodAnnotation != nil {
				var updatedPod corev1.Pod
				if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(pod), &updatedPod); err != nil {
					t.Fatalf("failed to get pod: %v", err)
				}
				val, ok := updatedPod.Annotations[jobset.InPlaceRestartAttemptKey]
				if !ok {
					t.Errorf("expected pod annotation %s, got none", *tc.wantPodAnnotation)
				} else if val != *tc.wantPodAnnotation {
					t.Errorf("expected pod annotation %s, got %s", *tc.wantPodAnnotation, val)
				}
			}

			// Verify exit code
			if tc.wantExitCode != nil {
				select {
				case exitCode := <-exitCodeChannel:
					if exitCode != *tc.wantExitCode {
						t.Errorf("expected exit code %d, got %d", *tc.wantExitCode, exitCode)
					}
				case <-time.After(100 * time.Millisecond):
					t.Errorf("expected exit code %d, but it timed out", *tc.wantExitCode)
				}
			} else {
				select {
				case code := <-exitCodeChannel:
					t.Errorf("expected no exit, got %d", code)
				default:
					// Success
				}
			}

			// Verify worker executed
			if tc.wantWorkerExecuted {
				select {
				case <-workerExecutedChannel:
					// Success
				case <-time.After(100 * time.Millisecond):
					t.Error("expected worker to be executed, but it timed out")
				}
			} else {
				select {
				case <-workerExecutedChannel:
					t.Error("expected worker not to be executed, but it was")
				default:
					// Success
				}
			}

			// Verify startup probe server started
			if tc.wantStartupProbeServerStarted {
				select {
				case <-startupProbeServerStartedChannel:
					// Success
				case <-time.After(100 * time.Millisecond):
					t.Error("expected startup probe server to be started, but it timed out")
				}
			} else {
				select {
				case <-startupProbeServerStartedChannel:
					t.Error("expected startup probe server not to be started, but it was")
				default:
					// Success
				}
			}
		})
	}
}
