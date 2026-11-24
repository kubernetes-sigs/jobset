package cert

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	certrotator "github.com/open-policy-agent/cert-controller/pkg/rotator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

func TestWebhookCertDir(t *testing.T) {
	tests := map[string]struct {
		cfg  configapi.Configuration
		want string
	}{
		"default cert dir": {
			cfg:  configapi.Configuration{},
			want: certDir,
		},
		"custom cert dir": {
			cfg: configapi.Configuration{
				ControllerManager: configapi.ControllerManager{
					Webhook: configapi.ControllerWebhook{CertDir: "/custom/certs"},
				},
			},
			want: "/custom/certs",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := webhookCertDir(tc.cfg); got != tc.want {
				t.Fatalf("webhookCertDir()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildCertRotatorConfig(t *testing.T) {
	readyCh := make(chan struct{})
	cfg := configapi.Configuration{
		ControllerManager: configapi.ControllerManager{
			Webhook: configapi.ControllerWebhook{CertDir: "/custom/certs"},
		},
		InternalCertManagement: &configapi.InternalCertManagement{
			Enable:             ptr.To(true),
			WebhookServiceName: ptr.To("jobset-webhook-service"),
			WebhookSecretName:  ptr.To("jobset-webhook-server-cert"),
		},
	}

	got := buildCertRotatorConfig(cfg, "jobset-system", "cert-rotator-bootstrap", readyCh, false)

	if got.SecretKey.Namespace != "jobset-system" {
		t.Fatalf("SecretKey.Namespace=%q, want %q", got.SecretKey.Namespace, "jobset-system")
	}
	if got.SecretKey.Name != "jobset-webhook-server-cert" {
		t.Fatalf("SecretKey.Name=%q, want %q", got.SecretKey.Name, "jobset-webhook-server-cert")
	}
	if got.CertDir != "/custom/certs" {
		t.Fatalf("CertDir=%q, want %q", got.CertDir, "/custom/certs")
	}
	if got.CAName != caName {
		t.Fatalf("CAName=%q, want %q", got.CAName, caName)
	}
	if got.CAOrganization != caOrg {
		t.Fatalf("CAOrganization=%q, want %q", got.CAOrganization, caOrg)
	}
	if got.DNSName != "jobset-webhook-service.jobset-system.svc" {
		t.Fatalf("DNSName=%q, want %q", got.DNSName, "jobset-webhook-service.jobset-system.svc")
	}
	if got.IsReady != readyCh {
		t.Fatalf("IsReady channel was not preserved")
	}
	if got.ControllerName != "cert-rotator-bootstrap" {
		t.Fatalf("ControllerName=%q, want %q", got.ControllerName, "cert-rotator-bootstrap")
	}

	wantWebhooks := []certrotator.WebhookInfo{
		{Type: certrotator.Validating, Name: validateWebhookConfName},
		{Type: certrotator.Mutating, Name: mutatingWebhookConfName},
	}
	if len(got.Webhooks) != len(wantWebhooks) {
		t.Fatalf("len(Webhooks)=%d, want %d", len(got.Webhooks), len(wantWebhooks))
	}
	for i := range wantWebhooks {
		if got.Webhooks[i] != wantWebhooks[i] {
			t.Fatalf("Webhooks[%d]=%+v, want %+v", i, got.Webhooks[i], wantWebhooks[i])
		}
	}
}

func TestBuildCertRotatorConfigWebhooks(t *testing.T) {
	cfg := configapi.Configuration{
		InternalCertManagement: &configapi.InternalCertManagement{
			Enable:             ptr.To(true),
			WebhookServiceName: ptr.To("jobset-webhook-service"),
			WebhookSecretName:  ptr.To("jobset-webhook-server-cert"),
		},
	}
	admissionWebhooks := []certrotator.WebhookInfo{
		{Type: certrotator.Validating, Name: validateWebhookConfName},
		{Type: certrotator.Mutating, Name: mutatingWebhookConfName},
	}

	tests := map[string]struct {
		injectConversion bool
		want             []certrotator.WebhookInfo
	}{
		"CRD without a conversion webhook": {
			want: admissionWebhooks,
		},
		"CRD with a conversion webhook": {
			injectConversion: true,
			want:             append(admissionWebhooks, certrotator.WebhookInfo{Type: certrotator.CRDConversion, Name: jobSetCRDName}),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildCertRotatorConfig(cfg, "jobset-system", "cert-rotator", make(chan struct{}), tc.injectConversion)
			if diff := cmp.Diff(tc.want, got.Webhooks); diff != "" {
				t.Fatalf("Webhooks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHasConversionWebhook(t *testing.T) {
	webhookConversion := func(clientConfig *apiextensionsv1.WebhookClientConfig) *apiextensionsv1.CustomResourceConversion {
		return &apiextensionsv1.CustomResourceConversion{
			Strategy: apiextensionsv1.WebhookConverter,
			Webhook:  &apiextensionsv1.WebhookConversion{ClientConfig: clientConfig},
		}
	}
	tests := map[string]struct {
		conversion *apiextensionsv1.CustomResourceConversion
		want       bool
	}{
		"no conversion": {},
		"None strategy": {
			conversion: &apiextensionsv1.CustomResourceConversion{Strategy: apiextensionsv1.NoneConverter},
		},
		"Webhook strategy without a client config": {
			conversion: webhookConversion(nil),
		},
		"Webhook strategy with a client config": {
			conversion: webhookConversion(&apiextensionsv1.WebhookClientConfig{}),
			want:       true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			crd := &apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{Conversion: tc.conversion},
			}
			if got := hasConversionWebhook(crd); got != tc.want {
				t.Fatalf("hasConversionWebhook()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestLookupConversionWebhook(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apiextensions to scheme: %v", err)
	}
	crdGR := schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"}
	crdWithWebhook := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: jobSetCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook:  &apiextensionsv1.WebhookConversion{ClientConfig: &apiextensionsv1.WebhookClientConfig{}},
			},
		},
	}
	failGet := func(err error) interceptor.Funcs {
		return interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return err
			},
		}
	}

	tests := map[string]struct {
		objs    []client.Object
		funcs   interceptor.Funcs
		want    bool
		wantErr bool
	}{
		"CRD with a conversion webhook": {
			objs: []client.Object{crdWithWebhook},
			want: true,
		},
		"CRD not found falls back to no injection": {},
		"forbidden falls back to no injection": {
			funcs: failGet(apierrors.NewForbidden(crdGR, jobSetCRDName, errors.New("no RBAC"))),
		},
		"other errors are returned": {
			funcs:   failGet(apierrors.NewServiceUnavailable("apiserver down")),
			wantErr: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).WithInterceptorFuncs(tc.funcs).Build()
			got, err := lookupConversionWebhook(context.Background(), c)
			if (err != nil) != tc.wantErr {
				t.Fatalf("lookupConversionWebhook() error=%v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("lookupConversionWebhook()=%v, want %v", got, tc.want)
			}
		})
	}
}
