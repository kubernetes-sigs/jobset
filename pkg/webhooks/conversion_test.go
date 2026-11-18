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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apix "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	jobsetv1 "sigs.k8s.io/jobset/api/jobset/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// newConversionHandler returns the /convert handler as the manager serves it.
func newConversionHandler(t *testing.T) http.Handler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, jobset.AddToScheme(scheme))
	require.NoError(t, jobsetv1.AddToScheme(scheme))

	converter, err := jobSetConverter(scheme)
	require.NoError(t, err)
	registry := conversion.NewRegistry()
	require.NoError(t, registry.RegisterConverter(jobset.GroupVersion.WithKind("JobSet").GroupKind(), converter))
	return conversion.NewWebhookHandler(scheme, registry)
}

// convert sends obj to the handler in a ConversionReview and returns the
// converted object, or the error message from the response.
func convert(t *testing.T, handler http.Handler, obj runtime.Object, desiredAPIVersion string) (map[string]any, string) {
	t.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	review := apix.ConversionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "ConversionReview"},
		Request: &apix.ConversionRequest{
			UID:               types.UID("uid"),
			DesiredAPIVersion: desiredAPIVersion,
			Objects:           []runtime.RawExtension{{Raw: raw}},
		},
	}
	body, err := json.Marshal(review)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/convert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp apix.ConversionReview
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Response)
	assert.Equal(t, types.UID("uid"), resp.Response.UID)
	if resp.Response.Result.Status != metav1.StatusSuccess {
		return nil, resp.Response.Result.Message
	}
	require.Len(t, resp.Response.ConvertedObjects, 1)
	var out map[string]any
	require.NoError(t, json.Unmarshal(resp.Response.ConvertedObjects[0].Raw, &out))
	return out, ""
}

func TestConversionWebhook(t *testing.T) {
	handler := newConversionHandler(t)

	hub := &jobset.JobSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: jobset.GroupVersion.String(), Kind: "JobSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{{Name: "leader"}, {Name: "workers"}},
			StartupPolicy:  &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder},
			FailurePolicy:  &jobset.FailurePolicy{RestartStrategy: jobset.BlockingRecreate},
		},
		Status: jobset.JobSetStatus{
			TerminalState:        string(jobset.JobSetCompleted),
			ReplicatedJobsStatus: []jobset.ReplicatedJobStatus{{Name: "leader", Succeeded: 1}},
		},
	}

	t.Run("v1alpha2 to v1", func(t *testing.T) {
		out, errMsg := convert(t, handler, hub, jobsetv1.GroupVersion.String())
		require.Empty(t, errMsg)
		assert.Equal(t, "jobset.x-k8s.io/v1", out["apiVersion"])
		assert.Equal(t, "JobSet", out["kind"])

		var got jobsetv1.JobSet
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(out, &got))
		assert.Equal(t, string(jobset.InOrder), got.Annotations[jobsetv1.StartupPolicyAnnotationKey])
		assert.Equal(t, []jobsetv1.DependsOn{{Name: "leader", Status: jobsetv1.DependencyReady}}, got.Spec.ReplicatedJobs[1].DependsOn)
		assert.Equal(t, jobsetv1.BlockingRecreate, got.Spec.FailurePolicy.RestartPolicy)
		assert.Equal(t, string(jobsetv1.JobSetComplete), got.Status.TerminalState)
		assert.Equal(t, int32(1), got.Status.ReplicatedJobsStatus[0].Complete)
	})

	t.Run("v1 to v1alpha2 restores the original object", func(t *testing.T) {
		spokeOut, errMsg := convert(t, handler, hub, jobsetv1.GroupVersion.String())
		require.Empty(t, errMsg)
		spoke := &jobsetv1.JobSet{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(spokeOut, spoke))

		out, errMsg := convert(t, handler, spoke, jobset.GroupVersion.String())
		require.Empty(t, errMsg)
		var got jobset.JobSet
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(out, &got))
		assert.Equal(t, hub, &got)
	})

	t.Run("conversion errors are reported in the response", func(t *testing.T) {
		invalid := hub.DeepCopy()
		invalid.Spec.ReplicatedJobs[1].DependsOn = []jobset.DependsOn{{Name: "leader", Status: jobset.DependencyComplete}}
		_, errMsg := convert(t, handler, invalid, jobsetv1.GroupVersion.String())
		assert.Contains(t, errMsg, "sets dependsOn together with the InOrder startupPolicy")
	})
}
