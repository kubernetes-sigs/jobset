/*
Copyright 2022 The Kubernetes Authors.

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

package v1alpha1

import (
	"time"

	configv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/utils/ptr"
)

const (
	DefaultWebhookServiceName                  = "jobset-webhook-service"
	DefaultWebhookSecretName                   = "jobset-webhook-server-cert"
	DefaultWebhookPort                         = 9443
	DefaultHealthProbeBindAddress              = ":8081"
	DefaultMetricsBindAddress                  = ":8443"
	DefaultLeaderElectionID                    = "6d4f6a47.jobset.x-k8s.io"
	DefaultLeaderElectionLeaseDuration         = 15 * time.Second
	DefaultLeaderElectionRenewDeadline         = 10 * time.Second
	DefaultLeaderElectionRetryPeriod           = 2 * time.Second
	DefaultResourceLock                        = "leases"
	DefaultClientConnectionQPS         float32 = 500
	DefaultClientConnectionBurst       int32   = 500
)

// SetDefaults_Configuration sets default values for ComponentConfig.
//
//nolint:revive // format required by generated code for defaulting
func SetDefaults_Configuration(cfg *Configuration) {
	if cfg.Webhook.Port == nil {
		cfg.Webhook.Port = ptr.To(DefaultWebhookPort)
	}
	if len(cfg.Metrics.BindAddress) == 0 {
		cfg.Metrics.BindAddress = DefaultMetricsBindAddress
	}
	if len(cfg.Health.HealthProbeBindAddress) == 0 {
		cfg.Health.HealthProbeBindAddress = DefaultHealthProbeBindAddress
	}

	if cfg.LeaderElection == nil {
		cfg.LeaderElection = &configv1alpha1.LeaderElectionConfiguration{}
	}
	if len(cfg.LeaderElection.ResourceName) == 0 {
		cfg.LeaderElection.ResourceName = DefaultLeaderElectionID
	}
	if len(cfg.LeaderElection.ResourceLock) == 0 {
		cfg.LeaderElection.ResourceLock = DefaultResourceLock
	}
	// Use the default LeaderElectionConfiguration options
	configv1alpha1.RecommendedDefaultLeaderElectionConfiguration(cfg.LeaderElection)

	if cfg.InternalCertManagement == nil {
		cfg.InternalCertManagement = &InternalCertManagement{}
	}
	if cfg.InternalCertManagement.Enable == nil {
		cfg.InternalCertManagement.Enable = ptr.To(true)
	}
	if *cfg.InternalCertManagement.Enable {
		if cfg.InternalCertManagement.WebhookServiceName == nil {
			cfg.InternalCertManagement.WebhookServiceName = ptr.To(DefaultWebhookServiceName)
		}
		if cfg.InternalCertManagement.WebhookSecretName == nil {
			cfg.InternalCertManagement.WebhookSecretName = ptr.To(DefaultWebhookSecretName)
		}
	}
	if cfg.ClientConnection == nil {
		cfg.ClientConnection = &ClientConnection{}
	}
	if cfg.ClientConnection.QPS == nil {
		cfg.ClientConnection.QPS = ptr.To(DefaultClientConnectionQPS)
	}
	if cfg.ClientConnection.Burst == nil {
		cfg.ClientConnection.Burst = ptr.To(DefaultClientConnectionBurst)
	}
}
