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

package cert

import (
	"fmt"

	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	serviceName             = "jobset-webhook-service"
	secretName              = "jobset-webhook-server-cert"
	secretNamespace         = "jobset-system"
	certDir                 = "/tmp/k8s-webhook-server/serving-certs"
	validateWebhookConfName = "jobset-validating-webhook-configuration"
	mutatingWebhookConfName = "jobset-mutating-webhook-configuration"
	caName                  = "jobset-ca"
	caOrg                   = "jobset"
)

// dnsName is the format of <service name>.<namespace>.svc
var dnsName = fmt.Sprintf("%s.%s.svc", serviceName, secretNamespace)

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch;update
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;update

// CertsManager creates certs for webhooks.
func CertsManager(mgr ctrl.Manager, setupFinish chan struct{}) error {
	return cert.AddRotator(mgr, &cert.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: secretNamespace,
			Name:      secretName,
		},
		CertDir:        certDir,
		CAName:         caName,
		CAOrganization: caOrg,
		DNSName:        dnsName,
		IsReady:        setupFinish,
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
	})
}
