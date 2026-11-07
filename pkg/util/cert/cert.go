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
	"time"

	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	config "sigs.k8s.io/jobset/api/config/v1alpha1"
)

const (
	defaultNamespace        = "jobset-system"
	certDir                 = "/tmp/k8s-webhook-server/serving-certs"
	validateWebhookConfName = "jobset-validating-webhook-configuration"
	mutatingWebhookConfName = "jobset-mutating-webhook-configuration"
	jobSetCRDName           = "jobsets.jobset.x-k8s.io"
	crdLookupTimeout        = 30 * time.Second
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
//+kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=update,resourceNames=jobsets.jobset.x-k8s.io

func webhookCertDir(cfg config.Configuration) string {
	if cfg.Webhook.CertDir != "" {
		return cfg.Webhook.CertDir
	}
	return certDir
}

// buildCertRotatorConfig returns the cert rotator configuration. When
// injectConversion is true, the CA is also injected into the conversion webhook
// of the JobSet CRD.
func buildCertRotatorConfig(cfg config.Configuration, namespace, controllerName string, setupFinish chan struct{}, injectConversion bool) *cert.CertRotator {
	webhooks := []cert.WebhookInfo{
		{
			Type: cert.Validating,
			Name: validateWebhookConfName,
		},
		{
			Type: cert.Mutating,
			Name: mutatingWebhookConfName,
		},
	}
	if injectConversion {
		webhooks = append(webhooks, cert.WebhookInfo{
			Type: cert.CRDConversion,
			Name: jobSetCRDName,
		})
	}
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
		Webhooks:       webhooks,
	}
}

// crdUsesConversionWebhook reports whether the installed JobSet CRD has a
// conversion webhook configured. The rotator must only inject the CA into the
// CRD when it does: a CRD without one (such as the Helm chart's) makes the
// injection fail, and the webhook server would never become ready.
func crdUsesConversionWebhook(ctx context.Context, kubeConfig *rest.Config) (bool, error) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return false, fmt.Errorf("add apiextensions to scheme: %w", err)
	}
	c, err := client.New(kubeConfig, client.Options{Scheme: scheme})
	if err != nil {
		return false, fmt.Errorf("create client: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, crdLookupTimeout)
	defer cancel()
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, types.NamespacedName{Name: jobSetCRDName}, crd); err != nil {
		return false, fmt.Errorf("get CRD %s: %w", jobSetCRDName, err)
	}
	return hasConversionWebhook(crd), nil
}

// hasConversionWebhook reports whether crd converts through a webhook with a
// client config the CA can be injected into.
func hasConversionWebhook(crd *apiextensionsv1.CustomResourceDefinition) bool {
	conversion := crd.Spec.Conversion
	return conversion != nil &&
		conversion.Strategy == apiextensionsv1.WebhookConverter &&
		conversion.Webhook != nil &&
		conversion.Webhook.ClientConfig != nil
}

// BootstrapCerts creates a minimal manager to generate webhook certificates and
// waits for them to be mounted before the main manager starts.
func BootstrapCerts(ctx context.Context, kubeConfig *rest.Config, cfg config.Configuration) error {
	namespace := getOperatorNamespace()
	injectConversion, err := crdUsesConversionWebhook(ctx, kubeConfig)
	if err != nil {
		return fmt.Errorf("check CRD conversion webhook: %w", err)
	}
	bootstrapMgr, err := ctrl.NewManager(kubeConfig, ctrl.Options{
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		return fmt.Errorf("create bootstrap manager: %w", err)
	}

	certsReady := make(chan struct{})
	if err := cert.AddRotator(bootstrapMgr, buildCertRotatorConfig(cfg, namespace, "cert-rotator-bootstrap", certsReady, injectConversion)); err != nil {
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
	injectConversion, err := crdUsesConversionWebhook(context.Background(), mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("check CRD conversion webhook: %w", err)
	}
	rotatorReady := make(chan struct{})
	if err := cert.AddRotator(mgr, buildCertRotatorConfig(cfg, namespace, "cert-rotator", rotatorReady, injectConversion)); err != nil {
		return err
	}
	close(setupFinish)
	return nil
}
