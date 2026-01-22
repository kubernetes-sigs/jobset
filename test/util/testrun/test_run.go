/*
Copyright 2025 The Kubernetes Authors.
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

package testrun

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/printers"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/test/util"
)

const (
	// DefaultNamespaceGenerateName is the default namespace generate name used when creating a testing namespace.
	DefaultNamespaceGenerateName = "jobset-ns-"

	// DefaultTimeout is the default timeout value used for gomega.Eventually.
	DefaultTimeout = 10 * time.Minute

	// DefaultInterval is the default interval value used for gomega.Eventually.
	DefaultInterval = time.Millisecond * 250
)

// TestRunOption is used to set a TestRun option. It can be passed to the constructor.
type TestRunOption func(*TestRun)

// SetNamespaceGenerateName sets the generateName for the namespace to be created for testing.
func SetNamespaceGenerateName(generateName string) TestRunOption {
	return func(run *TestRun) {
		run.namespaceGenerateName = generateName
	}
}

// DumpNamespace prints all JobSets and Jobs in the testing namespace before deleting the namespace, when enabled.
// It writes into ginkgo.GinkgoWriter, so it's only visible for failing tests or when a more verbose mode is being used.
func DumpNamespace(enabled bool) TestRunOption {
	return func(testRun *TestRun) {
		testRun.dumpNamespace = enabled
	}
}

// SetTimeout overwrites the default timeout value for gomega.Eventually.
func SetTimeout(timeout time.Duration) TestRunOption {
	return func(testRun *TestRun) {
		testRun.timeout = timeout
	}
}

// SetInterval overwrites the defautl interval value for gomega.Eventually.
func SetInterval(interval time.Duration) TestRunOption {
	return func(testRun *TestRun) {
		testRun.interval = interval
	}
}

// TestRun wraps common setup/teardown logic needed for a test run,
// particularly creating and deleting a testing namespace.
//
// New TestRun should be created before each test run, then calling TestRun.AfterEach to clean up resources.
// This mirrors BeforeEach/AfterEach being used in gomega.
type TestRun struct {
	ctx       context.Context
	k8sClient client.Client

	namespaceGenerateName string
	dumpNamespace         bool
	timeout               time.Duration
	interval              time.Duration

	Namespace *corev1.Namespace
}

// New creates a new TestRun using the given client and options.
func New(ctx context.Context, k8sClient client.Client, opts ...TestRunOption) *TestRun {
	testRun := &TestRun{
		ctx:                   ctx,
		k8sClient:             k8sClient,
		namespaceGenerateName: DefaultNamespaceGenerateName,
		timeout:               DefaultTimeout,
		interval:              DefaultInterval,
	}
	for _, opt := range opts {
		opt(testRun)
	}
	testRun.beforeEach()
	return testRun
}

func (testRun *TestRun) beforeEach() {
	// Create test namespace before each test.
	testRun.Namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: testRun.namespaceGenerateName,
		},
	}
	gomega.Expect(testRun.k8sClient.Create(testRun.ctx, testRun.Namespace)).To(gomega.Succeed())

	// Wait for namespace to exist before proceeding with test.
	gomega.Eventually(func() error {
		return testRun.k8sClient.Get(testRun.ctx, types.NamespacedName{Namespace: testRun.Namespace.Namespace, Name: testRun.Namespace.Name}, testRun.Namespace)
	}, testRun.timeout, testRun.interval).Should(gomega.Succeed())
}

// AfterEach cleans up resources created in the constructor.
func (testRun *TestRun) AfterEach() {
	// Dump the namespace content, optionally.
	// This is only visible on failure since ginkgo.GinkgoWriter is being used.
	if testRun.dumpNamespace {
		var printer printers.YAMLPrinter

		fmt.Fprintf(ginkgo.GinkgoWriter, "\nDumping relevant resources in namespace %s:\n\n", testRun.Namespace.Name)

		// JobSets
		var jobsets jobset.JobSetList
		gomega.Expect(testRun.k8sClient.List(testRun.ctx, &jobsets)).To(gomega.Succeed())
		for _, js := range jobsets.Items {
			gomega.Expect(printer.PrintObj(&js, ginkgo.GinkgoWriter)).To(gomega.Succeed())
		}

		// Jobs
		var jobs batchv1.JobList
		gomega.Expect(testRun.k8sClient.List(testRun.ctx, &jobs)).To(gomega.Succeed())
		for _, job := range jobs.Items {
			// GVK is not set properly for the list items.
			job.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   batchv1.SchemeGroupVersion.Group,
				Version: batchv1.SchemeGroupVersion.Version,
				Kind:    "Job",
			})
			gomega.Expect(printer.PrintObj(&job, ginkgo.GinkgoWriter)).To(gomega.Succeed())
		}
	}

	// Delete test namespace after each test.
	gomega.Expect(util.DeleteNamespace(testRun.ctx, testRun.k8sClient, testRun.Namespace)).To(gomega.Succeed())
}
