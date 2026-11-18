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
	"context"

	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	jobsetv1 "sigs.k8s.io/jobset/api/jobset/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// jobSetConverter serves the /convert endpoint for JobSets. v1alpha2 is the
// hub (storage) version; v1 converts to and from it. The manager scheme must
// have both versions registered.
var jobSetConverter = conversion.NewHubSpokeConverter(&jobset.JobSet{},
	conversion.NewSpokeConverter(&jobsetv1.JobSet{},
		func(_ context.Context, hub *jobset.JobSet, spoke *jobsetv1.JobSet) error {
			return jobsetv1.Convert_v1alpha2_JobSet_To_v1_JobSet(hub, spoke, nil)
		},
		func(_ context.Context, spoke *jobsetv1.JobSet, hub *jobset.JobSet) error {
			return jobsetv1.Convert_v1_JobSet_To_v1alpha2_JobSet(spoke, hub, nil)
		},
	),
)
