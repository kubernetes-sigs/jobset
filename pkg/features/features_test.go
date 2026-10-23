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

package features

import "testing"

func TestValidateFeatureGates(t *testing.T) {
	cases := map[string]struct {
		gangOfGangs bool
		wasAPI      bool
		wantErr     bool
	}{
		"both disabled is valid":                     {gangOfGangs: false, wasAPI: false, wantErr: false},
		"base API only is valid":                     {gangOfGangs: false, wasAPI: true, wantErr: false},
		"gang of gangs with base API is valid":       {gangOfGangs: true, wasAPI: true, wantErr: false},
		"gang of gangs without base API is rejected": {gangOfGangs: true, wasAPI: false, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			SetFeatureGateDuringTest(t, JobSetWorkloadAwareSchedulingAPI, tc.wasAPI)
			SetFeatureGateDuringTest(t, JobSetGangOfGangs, tc.gangOfGangs)
			err := ValidateFeatureGates()
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
