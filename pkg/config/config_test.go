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

package config

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

func TestLoad(t *testing.T) {
	testScheme := runtime.NewScheme()
	err := configapi.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()

	ctrlManagerConfigSpecOverWriteConfig := filepath.Join(tmpDir, "ctrl-manager-config-spec-overwrite.yaml")
	if err := os.WriteFile(ctrlManagerConfigSpecOverWriteConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :38081
metrics:
  bindAddress: :38080
leaderElection:
  leaderElect: true
  resourceName: test-id
webhook:
  port: 9444
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	certOverWriteConfig := filepath.Join(tmpDir, "cert-overwrite.yaml")
	if err := os.WriteFile(certOverWriteConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
internalCertManagement:
  enable: true
  webhookServiceName: jobset-tenant-a-webhook-service
  webhookSecretName: jobset-tenant-a-webhook-server-cert
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	disableCertOverWriteConfig := filepath.Join(tmpDir, "disable-cert-overwrite.yaml")
	if err := os.WriteFile(disableCertOverWriteConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
internalCertManagement:
  enable: false
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	leaderElectionDisabledConfig := filepath.Join(tmpDir, "leaderElection-disabled.yaml")
	if err := os.WriteFile(leaderElectionDisabledConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
leaderElection:
  leaderElect: false
webhook:
  port: 9443
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	clientConnectionConfig := filepath.Join(tmpDir, "clientConnection.yaml")
	if err := os.WriteFile(clientConnectionConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
clientConnection:
  qps: 50
  burst: 100
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	metricsCertConfig := filepath.Join(tmpDir, "metrics-cert.yaml")
	if err := os.WriteFile(metricsCertConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
  certDir: /tmp/my-metrics-server/my-serving-certs
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
internalCertManagement:
  enable: true
  webhookServiceName: jobset-tenant-a-webhook-service
  webhookSecretName: jobset-tenant-a-webhook-server-cert
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	metricsCertWithExternalCertsConfig := filepath.Join(tmpDir, "metrics-cert-with-external-certs.yaml")
	if err := os.WriteFile(metricsCertWithExternalCertsConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
  certDir: /tmp/my-metrics-server/my-serving-certs
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
internalCertManagement:
  enable: false
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	invalidConfig := filepath.Join(tmpDir, "invalid-config.yaml")
	if err := os.WriteFile(invalidConfig, []byte(`
apiVersion: config.jobset.x-k8s.io/v1alpha1
kind: Configuration
invalidField: invalidValue
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
leaderElection:
  leaderElect: true
  resourceName: 6d4f6a47.jobset.x-k8s.io
webhook:
  port: 9443
`), os.FileMode(0600)); err != nil {
		t.Fatal(err)
	}

	defaultControlOptions := ctrl.Options{
		HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
		Metrics: metricsserver.Options{
			BindAddress: configapi.DefaultMetricsBindAddress,
		},
		LeaderElection:             true,
		LeaderElectionID:           configapi.DefaultLeaderElectionID,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
		RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
		RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
		WebhookServer: &webhook.DefaultServer{
			Options: webhook.Options{
				Port: configapi.DefaultWebhookPort,
			},
		},
	}

	enableDefaultInternalCertManagement := &configapi.InternalCertManagement{
		Enable:             ptr.To(true),
		WebhookServiceName: ptr.To(configapi.DefaultWebhookServiceName),
		WebhookSecretName:  ptr.To(configapi.DefaultWebhookSecretName),
	}

	ctrlOptsCmpOpts := []cmp.Option{
		cmpopts.IgnoreUnexported(ctrl.Options{}),
		cmpopts.IgnoreUnexported(webhook.DefaultServer{}),
		cmpopts.IgnoreUnexported(ctrlcache.Options{}),
		cmpopts.IgnoreUnexported(net.ListenConfig{}),
		cmpopts.IgnoreFields(ctrl.Options{}, "Scheme", "Logger"),
	}

	// Ignore the controller manager section since it's side effect is checked against
	// the content of  the resulting options
	configCmpOpts := []cmp.Option{
		cmpopts.IgnoreFields(configapi.Configuration{}, "ControllerManager"),
	}

	defaultClientConnection := &configapi.ClientConnection{
		QPS:   ptr.To[float32](configapi.DefaultClientConnectionQPS),
		Burst: ptr.To[int32](configapi.DefaultClientConnectionBurst),
	}

	testcases := []struct {
		name              string
		configFile        string
		wantConfiguration configapi.Configuration
		wantOptions       ctrl.Options
		wantError         error
	}{
		{
			name:       "default config",
			configFile: "",
			wantConfiguration: configapi.Configuration{
				InternalCertManagement: enableDefaultInternalCertManagement,
				ClientConnection:       defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
				Metrics: metricsserver.Options{
					BindAddress: configapi.DefaultMetricsBindAddress,
				},
				LeaderElection:             true,
				LeaderElectionID:           configapi.DefaultLeaderElectionID,
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: configapi.DefaultWebhookPort,
					},
				},
			},
		},
		{
			name:       "bad path",
			configFile: ".",
			wantError: &fs.PathError{
				Op:   "read",
				Path: ".",
				Err:  errors.New("is a directory"),
			},
		},
		{
			name:       "ControllerManagerConfigurationSpec overwrite config",
			configFile: ctrlManagerConfigSpecOverWriteConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: enableDefaultInternalCertManagement,
				ClientConnection:       defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: ":38081",
				Metrics: metricsserver.Options{
					BindAddress: ":38080",
				},
				LeaderElection:             true,
				LeaderElectionID:           "test-id",
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: 9444,
					},
				},
			},
		},
		{
			name:       "cert options overwrite config",
			configFile: certOverWriteConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:             ptr.To(true),
					WebhookServiceName: ptr.To("jobset-tenant-a-webhook-service"),
					WebhookSecretName:  ptr.To("jobset-tenant-a-webhook-server-cert"),
				},
				ClientConnection: defaultClientConnection,
			},
			wantOptions: defaultControlOptions,
		},
		{
			name:       "disable cert overwrite config",
			configFile: disableCertOverWriteConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable: ptr.To(false),
				},
				ClientConnection: defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
				Metrics: metricsserver.Options{
					BindAddress: configapi.DefaultMetricsBindAddress,
					CertDir:     "/tmp/k8s-metrics-server/serving-certs",
					CertName:    "tls.crt",
					KeyName:     "tls.key",
				},
				LeaderElection:             true,
				LeaderElectionID:           configapi.DefaultLeaderElectionID,
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: configapi.DefaultWebhookPort,
					},
				},
			},
		},
		{
			name:       "leaderElection disabled config",
			configFile: leaderElectionDisabledConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: enableDefaultInternalCertManagement,
				ClientConnection:       defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
				Metrics: metricsserver.Options{
					BindAddress: configapi.DefaultMetricsBindAddress,
				},
				LeaderElectionID:           configapi.DefaultLeaderElectionID,
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				LeaderElection:             false,
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: configapi.DefaultWebhookPort,
					},
				},
			},
		},
		{
			name:       "clientConnection config",
			configFile: clientConnectionConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: enableDefaultInternalCertManagement,
				ClientConnection: &configapi.ClientConnection{
					QPS:   ptr.To[float32](50),
					Burst: ptr.To[int32](100),
				},
			},
			wantOptions: defaultControlOptions,
		},
		{
			name:       "metrics cert config",
			configFile: metricsCertConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable:             ptr.To(true),
					WebhookServiceName: ptr.To("jobset-tenant-a-webhook-service"),
					WebhookSecretName:  ptr.To("jobset-tenant-a-webhook-server-cert"),
				},
				ClientConnection: defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
				Metrics: metricsserver.Options{
					BindAddress: ":8443",
					CertDir:     "/tmp/my-metrics-server/my-serving-certs",
					CertName:    "tls.crt",
					KeyName:     "tls.key",
				},
				LeaderElection:             true,
				LeaderElectionID:           configapi.DefaultLeaderElectionID,
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: configapi.DefaultWebhookPort,
					},
				},
			},
		},

		{
			name:       "metrics cert config with external certs",
			configFile: metricsCertWithExternalCertsConfig,
			wantConfiguration: configapi.Configuration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configapi.GroupVersion.String(),
					Kind:       "Configuration",
				},
				InternalCertManagement: &configapi.InternalCertManagement{
					Enable: ptr.To(false),
				},
				ClientConnection: defaultClientConnection,
			},
			wantOptions: ctrl.Options{
				HealthProbeBindAddress: configapi.DefaultHealthProbeBindAddress,
				Metrics: metricsserver.Options{
					BindAddress: ":8443",
					CertDir:     "/tmp/my-metrics-server/my-serving-certs",
					CertName:    "tls.crt",
					KeyName:     "tls.key",
				},
				LeaderElection:             true,
				LeaderElectionID:           configapi.DefaultLeaderElectionID,
				LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
				LeaseDuration:              ptr.To(configapi.DefaultLeaderElectionLeaseDuration),
				RenewDeadline:              ptr.To(configapi.DefaultLeaderElectionRenewDeadline),
				RetryPeriod:                ptr.To(configapi.DefaultLeaderElectionRetryPeriod),
				WebhookServer: &webhook.DefaultServer{
					Options: webhook.Options{
						Port: configapi.DefaultWebhookPort,
					},
				},
			},
		},
		{
			name:       "invalid config",
			configFile: invalidConfig,
			wantError: runtime.NewStrictDecodingError([]error{
				errors.New("unknown field \"invalidField\""),
			}),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			options, cfg, err := Load(testScheme, tc.configFile)
			if tc.wantError == nil {
				if err != nil {
					t.Errorf("Unexpected error:%s", err)
				}
				if diff := cmp.Diff(tc.wantConfiguration, cfg, configCmpOpts...); diff != "" {
					t.Errorf("Unexpected config (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantOptions, options, ctrlOptsCmpOpts...); diff != "" {
					t.Errorf("Unexpected options (-want +got):\n%s", diff)
				}
			} else {
				if diff := cmp.Diff(tc.wantError.Error(), err.Error()); diff != "" {
					t.Errorf("Unexpected error (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestEncode(t *testing.T) {
	testScheme := runtime.NewScheme()
	err := configapi.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	defaultConfig := &configapi.Configuration{}
	testScheme.Default(defaultConfig)

	testcases := []struct {
		name       string
		scheme     *runtime.Scheme
		cfg        *configapi.Configuration
		wantResult map[string]any
	}{

		{
			name:   "empty",
			scheme: testScheme,
			cfg:    &configapi.Configuration{},
			wantResult: map[string]any{
				"apiVersion": "config.jobset.x-k8s.io/v1alpha1",
				"kind":       "Configuration",
				"health":     map[string]any{},
				"metrics":    map[string]any{},
				"webhook":    map[string]any{},
			},
		},
		{
			name:   "default",
			scheme: testScheme,
			cfg:    defaultConfig,
			wantResult: map[string]any{
				"apiVersion": "config.jobset.x-k8s.io/v1alpha1",
				"kind":       "Configuration",
				"webhook": map[string]any{
					"port": int64(configapi.DefaultWebhookPort),
				},
				"metrics": map[string]any{
					"bindAddress": configapi.DefaultMetricsBindAddress,
				},
				"health": map[string]any{
					"healthProbeBindAddress": configapi.DefaultHealthProbeBindAddress,
				},
				"leaderElection": map[string]any{
					"leaderElect":       true,
					"leaseDuration":     configapi.DefaultLeaderElectionLeaseDuration.String(),
					"renewDeadline":     configapi.DefaultLeaderElectionRenewDeadline.String(),
					"retryPeriod":       configapi.DefaultLeaderElectionRetryPeriod.String(),
					"resourceLock":      resourcelock.LeasesResourceLock,
					"resourceName":      configapi.DefaultLeaderElectionID,
					"resourceNamespace": "",
				},
				"internalCertManagement": map[string]any{
					"enable":             true,
					"webhookServiceName": configapi.DefaultWebhookServiceName,
					"webhookSecretName":  configapi.DefaultWebhookSecretName,
				},
				"clientConnection": map[string]any{
					"burst": int64(configapi.DefaultClientConnectionBurst),
					"qps":   int64(configapi.DefaultClientConnectionQPS),
				},
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.scheme, tc.cfg)
			if err != nil {
				t.Errorf("Unexpected error:%s", err)
			}
			gotMap := map[string]interface{}{}
			err = yaml.Unmarshal([]byte(got), &gotMap)
			if err != nil {
				t.Errorf("Unable to unmarshal result:%s", err)
			}
			if diff := cmp.Diff(tc.wantResult, gotMap); diff != "" {
				t.Errorf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}
