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
	"fmt"

	cliflag "k8s.io/component-base/cli/flag"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

const TLS12 = tls.VersionTLS12

// TLS holds the parsed TLS configuration with numeric constants
type TLS struct {
	MinVersion   uint16
	CipherSuites []uint16
}

// ParseTLSOptions converts configuration API TLS options to internal TLS struct
func ParseTLSOptions(cfg *configapi.TLSOptions) (*TLS, error) {
	if cfg == nil {
		return nil, nil
	}

	ret := &TLS{}
	var parseErr error

	// Parse MinVersion - collect errors like Kueue pattern
	version, err := ConvertTLSMinVersion(cfg.MinVersion)
	if err != nil {
		parseErr = err
	}
	ret.MinVersion = version

	// Set cipher suites
	if len(cfg.CipherSuites) > 0 {
		cipherSuites, err := ConvertCipherSuites(cfg.CipherSuites)
		if err != nil {
			if parseErr != nil {
				parseErr = fmt.Errorf("%w; %w", parseErr, err)
			} else {
				parseErr = err
			}
		}
		if err == nil && len(cipherSuites) > 0 {
			ret.CipherSuites = cipherSuites
		}
	}
	return ret, parseErr
}

// BuildTLSOptions converts TLS options to controller-runtime TLSOpts functions.
// If tlsOptions is nil, it returns nil.
func BuildTLSOptions(tlsOptions *TLS) []func(*tls.Config) {
	if tlsOptions == nil {
		return nil
	}

	var tlsOpts []func(*tls.Config)

	tlsOpts = append(tlsOpts, func(c *tls.Config) {
		c.MinVersion = tlsOptions.MinVersion
		c.CipherSuites = tlsOptions.CipherSuites
	})

	return tlsOpts
}

// ConvertTLSMinVersion converts a TLS version string to the corresponding uint16 constant
// using k8s.io/component-base/cli/flag for validation
func ConvertTLSMinVersion(tlsMinVersion string) (uint16, error) {
	if tlsMinVersion == "" {
		return TLS12, nil
	}
	// Explicitly reject TLS 1.0 and TLS 1.1 as insecure
	if tlsMinVersion == "VersionTLS10" || tlsMinVersion == "VersionTLS11" {
		return 0, fmt.Errorf("invalid minVersion: %s is not supported. Please use VersionTLS12 or VersionTLS13", tlsMinVersion)
	}
	version, err := cliflag.TLSVersion(tlsMinVersion)
	if err != nil {
		return 0, fmt.Errorf("invalid minVersion: %w. Please use VersionTLS12 or VersionTLS13", err)
	}
	return version, nil
}

// ConvertCipherSuites converts cipher suite names to their crypto/tls constants
// using k8s.io/component-base/cli/flag for validation
func ConvertCipherSuites(cipherSuites []string) ([]uint16, error) {
	if len(cipherSuites) == 0 {
		return nil, nil
	}
	suites, err := cliflag.TLSCipherSuites(cipherSuites)
	if err != nil {
		return nil, fmt.Errorf("invalid cipher suites: %w. Please use the secure cipher names: %v", err, cliflag.PreferredTLSCipherNames())
	}
	return suites, nil
}
