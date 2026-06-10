package cert

import (
	"testing"

	certrotator "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/utils/ptr"

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

	got := buildCertRotatorConfig(cfg, "jobset-system", "cert-rotator-bootstrap", readyCh)

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
