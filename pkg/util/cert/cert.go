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

package cert

import (
	"context"
	"fmt"
	"os"
	"strings"

	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	config "sigs.k8s.io/jobset/api/config/v1alpha1"
)

const (
	defaultNamespace        = "jobset-system"
	certDir                 = "/tmp/k8s-webhook-server/serving-certs"
	validateWebhookConfName = "jobset-validating-webhook-configuration"
	mutatingWebhookConfName = "jobset-mutating-webhook-configuration"
	caName                  = "jobset-ca"
	caOrg                   = "jobset"
)

func getOperatorNamespace() string {
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return defaultNamespace
}

//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch;update
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;update

func webhookCertDir(cfg config.Configuration) string {
	if cfg.Webhook.CertDir != "" {
		return cfg.Webhook.CertDir
	}
	return certDir
}

func buildCertRotatorConfig(cfg config.Configuration, namespace, controllerName string, setupFinish chan struct{}) *cert.CertRotator {
	return &cert.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: namespace,
			Name:      *cfg.InternalCertManagement.WebhookSecretName,
		},
		CertDir:        webhookCertDir(cfg),
		CAName:         caName,
		CAOrganization: caOrg,
		DNSName:        fmt.Sprintf("%s.%s.svc", *cfg.InternalCertManagement.WebhookServiceName, namespace),
		IsReady:        setupFinish,
		ControllerName: controllerName,
		Webhooks: []cert.WebhookInfo{
			{
				Type: cert.Validating,
				Name: validateWebhookConfName,
			},
			{
				Type: cert.Mutating,
				Name: mutatingWebhookConfName,
			},
		},
	}
}

// BootstrapCerts creates a minimal manager to generate webhook certificates and
// waits for them to be mounted before the main manager starts.
func BootstrapCerts(ctx context.Context, kubeConfig *rest.Config, cfg config.Configuration) error {
	namespace := getOperatorNamespace()
	bootstrapMgr, err := ctrl.NewManager(kubeConfig, ctrl.Options{
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		return fmt.Errorf("create bootstrap manager: %w", err)
	}

	certsReady := make(chan struct{})
	if err := cert.AddRotator(bootstrapMgr, buildCertRotatorConfig(cfg, namespace, "cert-rotator-bootstrap", certsReady)); err != nil {
		return fmt.Errorf("add bootstrap cert rotator: %w", err)
	}

	bootstrapCtx, bootstrapCancel := context.WithCancel(ctx)
	defer bootstrapCancel()

	startErr := make(chan error, 1)
	managerStopped := make(chan struct{})
	go func() {
		defer close(managerStopped)
		if err := bootstrapMgr.Start(bootstrapCtx); err != nil {
			startErr <- err
		}
	}()

	select {
	case <-certsReady:
	case err := <-startErr:
		return fmt.Errorf("start bootstrap manager: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	bootstrapCancel()
	select {
	case err := <-startErr:
		if err != nil {
			return fmt.Errorf("stop bootstrap manager: %w", err)
		}
	case <-managerStopped:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// CertsManager adds certificate rotation to the main manager after the initial
// certificate bootstrap is complete.
func CertsManager(mgr ctrl.Manager, cfg config.Configuration, setupFinish chan struct{}) error {
	namespace := getOperatorNamespace()
	rotatorReady := make(chan struct{})
	if err := cert.AddRotator(mgr, buildCertRotatorConfig(cfg, namespace, "cert-rotator", rotatorReady)); err != nil {
		return err
	}
	close(setupFinish)
	return nil
}
