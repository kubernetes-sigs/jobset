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

package scheduling

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 5 * time.Minute
	interval = time.Millisecond * 250
)

var (
	k8sClient client.Client
	ctx       context.Context
)

func TestSchedulingE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Scheduling E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	ctx = context.Background()
	cfg := config.GetConfigOrDie()
	gomega.ExpectWithOffset(1, cfg).NotTo(gomega.BeNil())

	err := jobset.AddToScheme(scheme.Scheme)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	err = batchv1.AddToScheme(scheme.Scheme)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	err = schedulingv1alpha3.AddToScheme(scheme.Scheme)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(k8sClient).NotTo(gomega.BeNil())

	util.JobSetReadyForTesting(ctx, k8sClient)
})
