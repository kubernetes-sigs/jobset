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

package e2e

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	jobSetCRDName           = "jobsets.jobset.x-k8s.io"
	validatingWebhookConfig = "jobset-validating-webhook-configuration"
)

var _ = ginkgo.Describe("JobSet CRD conversion webhook", func() {
	ginkgo.It("should have the internal cert CA injected", func() {
		ginkgo.By("reading the CA injected into the validating webhook")
		vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: validatingWebhookConfig}, vwc)).To(gomega.Succeed())
		gomega.Expect(vwc.Webhooks).NotTo(gomega.BeEmpty())
		wantCABundle := vwc.Webhooks[0].ClientConfig.CABundle
		gomega.Expect(wantCABundle).NotTo(gomega.BeEmpty())

		ginkgo.By("checking the same CA is injected into the CRD conversion webhook")
		gomega.Eventually(func(g gomega.Gomega) {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobSetCRDName}, crd)).To(gomega.Succeed())
			g.Expect(crd.Spec.Conversion).NotTo(gomega.BeNil())
			g.Expect(crd.Spec.Conversion.Strategy).To(gomega.Equal(apiextensionsv1.WebhookConverter))
			g.Expect(crd.Spec.Conversion.Webhook).NotTo(gomega.BeNil())
			g.Expect(crd.Spec.Conversion.Webhook.ClientConfig).NotTo(gomega.BeNil())
			g.Expect(crd.Spec.Conversion.Webhook.ClientConfig.CABundle).To(gomega.Equal(wantCABundle))
		}, timeout, interval).Should(gomega.Succeed())
	})
})
