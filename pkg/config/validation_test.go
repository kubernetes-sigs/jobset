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

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

func TestValidate(t *testing.T) {
	testCases := map[string]struct {
		cfg     *configapi.Configuration
		wantErr field.ErrorList
	}{
		"invalid .internalCertManagement.webhookSecretName": {
			cfg: &configapi.Configuration{
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:            ptr.To(true),
					WebhookSecretName: ptr.To(":)"),
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "internalCertManagement.webhookSecretName",
				},
			},
		},
		"invalid .internalCertManagement.webhookServiceName": {
			cfg: &configapi.Configuration{
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:             ptr.To(true),
					WebhookServiceName: ptr.To("0-invalid"),
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "internalCertManagement.webhookServiceName",
				},
			},
		},
		"disabled .internalCertManagement with invalid .internalCertManagement.webhookServiceName": {
			cfg: &configapi.Configuration{
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:             ptr.To(false),
					WebhookServiceName: ptr.To("0-invalid"),
				},
			},
		},
		"valid .internalCertManagement": {
			cfg: &configapi.Configuration{
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:             ptr.To(true),
					WebhookServiceName: ptr.To("webhook-svc"),
					WebhookSecretName:  ptr.To("webhook-sec"),
				},
			},
		},
		"valid TLS with TLS 1.2 and cipher suites": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS12",
						CipherSuites: []string{
							"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
						},
					},
				},
			},
		},
		"valid TLS with TLS 1.3 and no cipher suites": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS13",
					},
				},
			},
		},
		"invalid TLS with TLS 1.3 and cipher suites": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS13",
						CipherSuites: []string{
							"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
						},
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls.cipherSuites",
				},
			},
		},
		"invalid TLS with unsupported minVersion VersionTLS10": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS10",
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls",
				},
			},
		},
		"invalid TLS with unsupported minVersion VersionTLS11": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS11",
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls",
				},
			},
		},
		"invalid TLS with invalid minVersion string": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "InvalidVersion",
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls",
				},
			},
		},
		"invalid TLS with invalid cipher suite name": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS12",
						CipherSuites: []string{
							"INVALID_CIPHER_SUITE",
						},
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls",
				},
			},
		},
		"invalid TLS with invalid minVersion and invalid cipher suite": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS10",
						CipherSuites: []string{
							"INVALID_CIPHER_SUITE",
						},
					},
				},
			},
			wantErr: field.ErrorList{
				&field.Error{
					Type:  field.ErrorTypeInvalid,
					Field: "tls",
				},
			},
		},
		"valid TLS with empty cipher suites": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion:   "VersionTLS12",
						CipherSuites: []string{},
					},
				},
			},
		},
		"valid TLS with multiple valid cipher suites": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{
						MinVersion: "VersionTLS12",
						CipherSuites: []string{
							"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
							"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
						},
					},
				},
			},
		},
		"nil TLS config is valid": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: nil,
				},
			},
		},
		"empty TLS config is valid": {
			cfg: &configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					TLS: &configapi.TLSOptions{},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.wantErr, validate(tc.cfg), cmpopts.IgnoreFields(field.Error{}, "BadValue", "Detail")); diff != "" {
				t.Errorf("Unexpected returned error (-want,+got):\n%s", diff)
			}
		})
	}
}
