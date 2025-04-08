/*
Copyright 2023 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    htcp://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestValidatePodPlacements(t *testing.T) {
	var (
		jobSetName       = "test-jobset"
		ns               = "default"
		leaderPodWrapper = makePod(&makePodArgs{
			jobSetName:        jobSetName,
			replicatedJobName: "replicated-job-1",
			jobName:           "test-jobset-replicated-job-1-test-job-0",
			podName:           "test-jobset-replicated-job-1-test-job-0-0",
			ns:                ns,
			nodeName:          "test-node",
			jobIdx:            0}).AddAnnotation(batchv1.JobCompletionIndexAnnotation, "0")
		followerPodWrapper = makePod(&makePodArgs{
			jobSetName:        jobSetName,
			replicatedJobName: "replicated-job-1",
			jobName:           "test-jobset-replicated-job-1-test-job-0",
			podName:           "test-jobset-replicated-job-1-test-job-0-1",
			ns:                ns,
			jobIdx:            0}).AddAnnotation(batchv1.JobCompletionIndexAnnotation, "1")
		testTopologyKey = "test-node-topologyKey"
		nodeSelector    = map[string]string{testTopologyKey: "topologyDomain"}
	)
	tests := []struct {
		name           string
		leaderPod      corev1.Pod
		podList        *corev1.PodList
		node           *corev1.Node
		wantErr        error
		forceClientErr bool
		wantMatched    bool
	}{
		{
			name:      "topology node label not found",
			leaderPod: leaderPodWrapper.AddAnnotation(jobset.ExclusiveKey, testTopologyKey).Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(nodeSelector).Obj(),
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			wantErr: fmt.Errorf("node does not have topology label: %s", testTopologyKey),
		},
		{
			name:      "valid pod placements",
			leaderPod: leaderPodWrapper.AddAnnotation(jobset.ExclusiveKey, testTopologyKey).Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(nodeSelector).Obj(),
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: nodeSelector,
				},
			},
			wantMatched: true,
		},
		{
			name:      "follower pod nodeSelector is nil",
			leaderPod: leaderPodWrapper.AddAnnotation(jobset.ExclusiveKey, testTopologyKey).Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(nil).Obj(),
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: nodeSelector,
				},
			},
			wantErr: fmt.Errorf("pod %s nodeSelector is nil", "test-jobset-replicated-job-1-test-job-0-1"),
		},
		{
			name:      "follower pod nodeSelector is empty",
			leaderPod: leaderPodWrapper.AddAnnotation(jobset.ExclusiveKey, testTopologyKey).Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(map[string]string{}).Obj(),
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: nodeSelector,
				},
			},
			wantErr: fmt.Errorf("pod %s nodeSelector is missing key: %s",
				"test-jobset-replicated-job-1-test-job-0-1", testTopologyKey),
		},
		{
			name:      "followerTopology != leaderTopology",
			leaderPod: leaderPodWrapper.Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(map[string]string{testTopologyKey: "topologyDomain1"}).Obj(),
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: nodeSelector,
				},
			},
			wantErr: fmt.Errorf("follower topology %q != leader topology %q",
				"topologyDomain1", "topologyDomain"),
		},
		{
			name:      "get node error",
			leaderPod: leaderPodWrapper.AddAnnotation(jobset.ExclusiveKey, testTopologyKey).Obj(),
			podList: &corev1.PodList{
				Items: []corev1.Pod{
					leaderPodWrapper.Obj(),
					followerPodWrapper.NodeSelector(map[string]string{testTopologyKey: "topologyDomain"}).Obj(),
				},
			},
			forceClientErr: true,
			wantErr:        errors.New("example error"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := makeFakeClient(interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch,
					key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					node, ok := obj.(*corev1.Node)
					if !ok {
						return nil
					}
					if tc.forceClientErr || node == nil {
						return errors.New("example error")
					}
					// Set returned node value to be the node defined in the test case.
					*node = *tc.node
					return nil
				}})
			r := &PodReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			gotMatched, gotErr := r.validatePodPlacements(context.Background(), &tc.leaderPod, tc.podList)
			if tc.wantErr != nil && gotErr != nil {
				assert.Equal(t, tc.wantErr.Error(), gotErr.Error())
			}
			assert.Equal(t, tc.wantMatched, gotMatched)
		})
	}
}

func TestDeleteFollowerPods(t *testing.T) {
	var (
		jobSetName       = "test-jobset"
		ns               = "default"
		leaderPodWrapper = makePod(&makePodArgs{
			jobSetName:        jobSetName,
			replicatedJobName: "replicated-job-1",
			jobName:           "test-jobset-replicated-job-1-test-job-0",
			podName:           "test-jobset-replicated-job-1-test-job-0-0",
			ns:                ns,
			nodeName:          "test-node",
			jobIdx:            0}).
			AddAnnotation(batchv1.JobCompletionIndexAnnotation, "0")
		followerPodWrapper = makePod(&makePodArgs{
			jobSetName:        jobSetName,
			replicatedJobName: "replicated-job-1",
			jobName:           "test-jobset-replicated-job-1-test-job-0",
			podName:           "test-jobset-replicated-job-1-test-job-0-1",
			ns:                ns,
			jobIdx:            0}).AddAnnotation(batchv1.JobCompletionIndexAnnotation, "1")
	)
	tests := []struct {
		name                 string
		pods                 []corev1.Pod
		wantPodsDeletedCount int
		wantErr              error
		forceClientErr       bool
	}{
		{
			name: "delete follower pods",
			pods: []corev1.Pod{
				leaderPodWrapper.Obj(),
				followerPodWrapper.Obj(),
			},
			wantPodsDeletedCount: 1,
		},
		{
			name: "delete follower pods with pod conditions status is false",
			pods: []corev1.Pod{
				leaderPodWrapper.Obj(),
				followerPodWrapper.SetConditions([]corev1.PodCondition{{
					Type:               corev1.DisruptionTarget,
					Status:             corev1.ConditionFalse,
					Reason:             constants.ExclusivePlacementViolationReason,
					LastTransitionTime: metav1.Now(),
					Message:            constants.ExclusivePlacementViolationMessage,
				},
				}).Obj(),
			},
			wantPodsDeletedCount: 1,
		},
		{
			name: "delete follower pods with update pod status error",
			pods: []corev1.Pod{
				leaderPodWrapper.Obj(),
				followerPodWrapper.SetConditions([]corev1.PodCondition{{
					Type:               corev1.DisruptionTarget,
					Status:             corev1.ConditionFalse,
					Reason:             constants.ExclusivePlacementViolationReason,
					LastTransitionTime: metav1.Now(),
					Message:            constants.ExclusivePlacementViolationMessage,
				},
				}).Obj(),
			},
			forceClientErr: true,
			wantErr:        errors.New("example error"),
		},
		{
			name: "delete follower pods with delete error",
			pods: []corev1.Pod{
				leaderPodWrapper.Obj(),
				followerPodWrapper.SetConditions([]corev1.PodCondition{{
					Type:    corev1.DisruptionTarget,
					Status:  corev1.ConditionTrue,
					Reason:  constants.ExclusivePlacementViolationReason,
					Message: constants.ExclusivePlacementViolationMessage},
				}).Obj(),
			},
			forceClientErr: true,
			wantErr:        errors.New("example error"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var deletedCount int
			fc := makeFakeClient(interceptor.Funcs{
				Delete: func(ctx context.Context, client client.WithWatch,
					obj client.Object, opts ...client.DeleteOption) error {
					pod, ok := obj.(*corev1.Pod)
					if !ok {
						return nil
					}
					if tc.forceClientErr || pod == nil {
						return errors.New("example error")
					}
					deletedCount++
					return nil
				},
				SubResourceUpdate: func(ctx context.Context, client client.Client,
					subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					pod, ok := obj.(*corev1.Pod)
					if !ok {
						return nil
					}
					if tc.forceClientErr || pod == nil {
						return errors.New("example error")
					}
					return nil
				},
			}, &tc.pods[0], &tc.pods[1])
			r := &PodReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}

			gotErr := r.deleteFollowerPods(context.Background(), tc.pods)
			if tc.wantErr != nil && gotErr != nil {
				assert.Equal(t, tc.wantErr.Error(), gotErr.Error())
			}
			if tc.wantPodsDeletedCount != deletedCount {
				t.Errorf("deleteFollowerPods() did not make the expected pod deletion calls")
			}
		})
	}
}

type makePodArgs struct {
	jobSetName        string
	jobSetUID         string
	replicatedJobName string
	jobName           string
	podName           string
	nodeName          string
	ns                string
	jobIdx            int
}

// Helper function to create a Pod for unit testing.
func makePod(args *makePodArgs) *testutils.PodWrapper {
	labels := map[string]string{
		jobset.JobSetNameKey:        args.jobSetName,
		jobset.JobSetUIDKey:         args.jobSetUID,
		jobset.ReplicatedJobNameKey: args.replicatedJobName,
		jobset.JobIndexKey:          strconv.Itoa(args.jobIdx),
		jobset.JobKey:               jobHashKey(args.ns, args.jobName),
	}
	annotations := map[string]string{
		jobset.JobSetNameKey: args.jobSetName,
		jobset.JobSetUIDKey:  args.jobSetUID,
		jobset.JobIndexKey:   strconv.Itoa(args.jobIdx),
		jobset.JobKey:        jobHashKey(args.ns, args.jobName),
	}
	return testutils.MakePod(args.podName, args.ns).Labels(labels).Annotations(annotations)
}

type fakeClient struct {
	client client.WithWatch
	scheme *runtime.Scheme
	record record.EventRecorder
}

func makeFakeClient(interceptor interceptor.Funcs, initObjs ...client.Object) *fakeClient {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))

	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})
	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjs...).
		WithInterceptorFuncs(interceptor).
		WithStatusSubresource(initObjs...).
		Build()
	return &fakeClient{
		client: fc,
		scheme: scheme,
		record: recorder,
	}
}
