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

package tlsconfig

import (
	"crypto/tls"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

// containsError checks if the error message contains the expected substring
func containsError(err error, substr string) bool {
	if err == nil {
		return substr == ""
	}
	return strings.Contains(err.Error(), substr)
}

func TestParseTLSOptions(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *configapi.TLSOptions
		expectedMinVer uint16
		expectedCipher []uint16
		expectNil      bool
		wantErr        bool
		wantErrSubstr  string
	}{
		{
			name:           "nil config",
			cfg:            nil,
			expectedMinVer: 0,
			expectedCipher: nil,
			expectNil:      true,
			wantErr:        false,
		},
		{
			name:           "empty config",
			cfg:            &configapi.TLSOptions{},
			expectedMinVer: tls.VersionTLS12,
			expectedCipher: nil,
			wantErr:        false,
		},
		{
			name: "no tls min version but cipher config",
			cfg: &configapi.TLSOptions{
				CipherSuites: []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				},
			},
			expectedMinVer: tls.VersionTLS12,
			expectedCipher: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
			wantErr: false,
		},
		{
			name: "TLS 1.2",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS12",
			},
			expectedMinVer: tls.VersionTLS12,
			expectedCipher: nil,
			wantErr:        false,
		},
		{
			name: "TLS 1.3",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS13",
			},
			expectedMinVer: tls.VersionTLS13,
			expectedCipher: nil,
			wantErr:        false,
		},
		{
			name: "with cipher suites",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS12",
				CipherSuites: []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				},
			},
			expectedMinVer: tls.VersionTLS12,
			expectedCipher: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
			wantErr: false,
		},
		{
			name: "TLS 1.3 with cipher suites",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS13",
				CipherSuites: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
				},
			},
			expectedMinVer: tls.VersionTLS13,
			expectedCipher: []uint16{
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_AES_256_GCM_SHA384,
			},
			wantErr: false,
		},
		{
			name: "TLS 1.0 is rejected",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS10",
			},
			wantErr:       true,
			wantErrSubstr: "VersionTLS10 is not supported",
		},
		{
			name: "TLS 1.1 is rejected",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS11",
			},
			wantErr:       true,
			wantErrSubstr: "VersionTLS11 is not supported",
		},
		{
			name: "TLS 1.0 with cipher suites",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS10",
				CipherSuites: []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				},
			},
			wantErr:       true,
			wantErrSubstr: "VersionTLS10 is not supported",
		},
		{
			name: "invalid TLS version returns error",
			cfg: &configapi.TLSOptions{
				MinVersion: "InvalidVersion",
			},
			wantErr:       true,
			wantErrSubstr: "invalid minVersion",
		},
		{
			name: "TLS 1.2 with invalid cipher suites",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS12",
				CipherSuites: []string{
					"INVALID_CIPHER_SUITE",
				},
			},
			wantErr:       true,
			wantErrSubstr: "invalid cipher suites",
		},
		{
			name: "TLS 1.0 with invalid cipher suites returns both errors",
			cfg: &configapi.TLSOptions{
				MinVersion: "VersionTLS10",
				CipherSuites: []string{
					"INVALID_CIPHER_SUITE",
				},
			},
			wantErr:       true,
			wantErrSubstr: "VersionTLS10 is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsOpts, err := ParseTLSOptions(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTLSOptions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.wantErrSubstr != "" {
				if !containsError(err, tt.wantErrSubstr) {
					t.Errorf("ParseTLSOptions() error = %v, want error containing %q", err, tt.wantErrSubstr)
				}
				return
			}

			if tt.expectNil {
				if tlsOpts != nil {
					t.Errorf("ParseTLSOptions() returned non-nil, want nil")
				}
				return
			}

			if tlsOpts.MinVersion != tt.expectedMinVer {
				t.Errorf("MinVersion = %v, want %v", tlsOpts.MinVersion, tt.expectedMinVer)
			}

			if !cmp.Equal(tlsOpts.CipherSuites, tt.expectedCipher) {
				t.Errorf("CipherSuites diff: %s", cmp.Diff(tt.expectedCipher, tlsOpts.CipherSuites))
			}
		})
	}
}

func TestConvertCipherSuites(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []uint16
		wantErr  bool
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: nil,
			wantErr:  false,
		},
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
			wantErr:  false,
		},
		{
			name: "valid cipher suites",
			input: []string{
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			},
			expected: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			},
			wantErr: false,
		},
		{
			name: "invalid cipher suite",
			input: []string{
				"INVALID_CIPHER_SUITE",
			},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertCipherSuites(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertCipherSuites() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !cmp.Equal(got, tt.expected) {
				t.Errorf("ConvertCipherSuites() diff: %s", cmp.Diff(tt.expected, got))
			}
		})
	}
}

func TestConvertTLSMinVersion(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      uint16
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:     "TLS 1.2",
			input:    "VersionTLS12",
			expected: tls.VersionTLS12,
			wantErr:  false,
		},
		{
			name:     "TLS 1.3",
			input:    "VersionTLS13",
			expected: tls.VersionTLS13,
			wantErr:  false,
		},
		{
			name:     "empty version defaults to TLS 1.2",
			input:    "",
			expected: tls.VersionTLS12,
			wantErr:  false,
		},
		{
			name:          "invalid version returns error",
			input:         "InvalidVersion",
			expected:      0,
			wantErr:       true,
			wantErrSubstr: "invalid minVersion",
		},
		{
			name:          "TLS 1.0 is rejected",
			input:         "VersionTLS10",
			expected:      0,
			wantErr:       true,
			wantErrSubstr: "VersionTLS10 is not supported",
		},
		{
			name:          "TLS 1.1 is rejected",
			input:         "VersionTLS11",
			expected:      0,
			wantErr:       true,
			wantErrSubstr: "VersionTLS11 is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertTLSMinVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertTLSMinVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.wantErrSubstr != "" {
				if !containsError(err, tt.wantErrSubstr) {
					t.Errorf("ConvertTLSMinVersion() error = %v, want error containing %q", err, tt.wantErrSubstr)
				}
				return
			}
			if got != tt.expected {
				t.Errorf("ConvertTLSMinVersion() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildTLSOptions(t *testing.T) {
	tests := []struct {
		name       string
		tlsOptions *TLS
		wantNil    bool
		validateFn func(*testing.T, []func(*tls.Config))
	}{
		{
			name:       "nil TLS options",
			tlsOptions: nil,
			wantNil:    true,
		},
		{
			name: "TLS 1.2 with no cipher suites",
			tlsOptions: &TLS{
				MinVersion:   tls.VersionTLS12,
				CipherSuites: nil,
			},
			wantNil: false,
			validateFn: func(t *testing.T, opts []func(*tls.Config)) {
				if len(opts) != 1 {
					t.Errorf("expected 1 TLS option function, got %d", len(opts))
					return
				}
				cfg := &tls.Config{}
				opts[0](cfg)
				if cfg.MinVersion != tls.VersionTLS12 {
					t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
				}
				if cfg.CipherSuites != nil {
					t.Errorf("CipherSuites = %v, want nil", cfg.CipherSuites)
				}
			},
		},
		{
			name: "TLS 1.3 with no cipher suites",
			tlsOptions: &TLS{
				MinVersion:   tls.VersionTLS13,
				CipherSuites: nil,
			},
			wantNil: false,
			validateFn: func(t *testing.T, opts []func(*tls.Config)) {
				if len(opts) != 1 {
					t.Errorf("expected 1 TLS option function, got %d", len(opts))
					return
				}
				cfg := &tls.Config{}
				opts[0](cfg)
				if cfg.MinVersion != tls.VersionTLS13 {
					t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS13)
				}
				if cfg.CipherSuites != nil {
					t.Errorf("CipherSuites = %v, want nil", cfg.CipherSuites)
				}
			},
		},
		{
			name: "TLS 1.2 with cipher suites",
			tlsOptions: &TLS{
				MinVersion: tls.VersionTLS12,
				CipherSuites: []uint16{
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				},
			},
			wantNil: false,
			validateFn: func(t *testing.T, opts []func(*tls.Config)) {
				if len(opts) != 1 {
					t.Errorf("expected 1 TLS option function, got %d", len(opts))
					return
				}
				cfg := &tls.Config{}
				opts[0](cfg)
				if cfg.MinVersion != tls.VersionTLS12 {
					t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
				}
				expectedCiphers := []uint16{
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				}
				if !cmp.Equal(cfg.CipherSuites, expectedCiphers) {
					t.Errorf("CipherSuites diff: %s", cmp.Diff(expectedCiphers, cfg.CipherSuites))
				}
			},
		},
		{
			name: "empty cipher suites slice",
			tlsOptions: &TLS{
				MinVersion:   tls.VersionTLS12,
				CipherSuites: []uint16{},
			},
			wantNil: false,
			validateFn: func(t *testing.T, opts []func(*tls.Config)) {
				if len(opts) != 1 {
					t.Errorf("expected 1 TLS option function, got %d", len(opts))
					return
				}
				cfg := &tls.Config{}
				opts[0](cfg)
				if cfg.MinVersion != tls.VersionTLS12 {
					t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
				}
				if !cmp.Equal(cfg.CipherSuites, []uint16{}) {
					t.Errorf("CipherSuites = %v, want empty slice", cfg.CipherSuites)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildTLSOptions(tt.tlsOptions)

			if tt.wantNil {
				if got != nil {
					t.Errorf("BuildTLSOptions() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Error("BuildTLSOptions() = nil, want non-nil")
				return
			}

			if tt.validateFn != nil {
				tt.validateFn(t, got)
			}
		})
	}
}
