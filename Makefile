# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO_CMD ?= go
GO_FMT ?= gofmt

# Use go.mod go version as a single source of truth of GO version.
GO_VERSION := $(shell awk '/^go /{print $$2}' go.mod|head -n1)

GIT_TAG ?= $(shell git describe --tags --dirty --always)
# Update for each release branch
BRANCH_NAME = main
# Image URL to use all building/pushing image targets
PLATFORMS ?= linux/amd64,linux/arm64,linux/s390x
DOCKER_BUILDX_CMD ?= docker buildx
IMAGE_BUILD_CMD ?= $(DOCKER_BUILDX_CMD) build
IMAGE_BUILD_EXTRA_OPTS ?=
STAGING_IMAGE_REGISTRY := us-central1-docker.pkg.dev/k8s-staging-images
IMAGE_REGISTRY ?= $(STAGING_IMAGE_REGISTRY)/jobset
IMAGE_NAME := jobset
IMAGE_REPO ?= $(IMAGE_REGISTRY)/$(IMAGE_NAME)
IMAGE_TAG ?= $(IMAGE_REPO):$(GIT_TAG)
HELM_CHART_REPO := $(STAGING_IMAGE_REGISTRY)/jobset/charts

# In-place restart agent image
# TODO (beta of in-place restart): Default IN_PLACE_RESTART_AGENT_IMAGE_REGISTRY to a valid registry URL to build and push the agent automatically
IN_PLACE_RESTART_AGENT_IMAGE_REGISTRY ?=
IN_PLACE_RESTART_AGENT_IMAGE_NAME := in-place-restart-agent
IN_PLACE_RESTART_AGENT_IMAGE_REPO ?= $(IN_PLACE_RESTART_AGENT_IMAGE_REGISTRY)/$(IN_PLACE_RESTART_AGENT_IMAGE_NAME)
IN_PLACE_RESTART_AGENT_IMAGE_TAG ?= $(IN_PLACE_RESTART_AGENT_IMAGE_REPO):$(GIT_TAG)
IN_PLACE_RESTART_AGENT_DOCKERFILE ?= cmd/in-place-restart-agent/Dockerfile

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
BASE_IMAGE ?= gcr.io/distroless/static:nonroot
BUILDER_IMAGE ?= golang:$(GO_VERSION)
CGO_ENABLED ?= 0

ARTIFACTS ?= $(PROJECT_DIR)/bin

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

INTEGRATION_TARGET ?= ./test/integration/...

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
JOBSET_CHART_DIR := charts/jobset

E2E_TARGET ?= ./test/e2e/...
E2E_KIND_VERSION ?= kindest/node:v1.34.0
USE_EXISTING_CLUSTER ?= false
# E2E config folder to use (e.g., "default", "certmanager", etc.)
E2E_TARGET_FOLDER ?= default

# For local testing, we should allow user to use different kind cluster name
# Default will delete default kind cluster
KIND_CLUSTER_NAME ?= kind

version_pkg = sigs.k8s.io/jobset/pkg/version
LD_FLAGS += -X '$(version_pkg).GitVersion=$(GIT_TAG)'
LD_FLAGS += -X '$(version_pkg).GitCommit=$(shell git rev-parse HEAD)'

# Setting SED allows macos users to install GNU sed and use the latter
# instead of the default BSD sed.
ifeq ($(shell command -v gsed 2>/dev/null),)
    SED ?= $(shell command -v sed)
else
    SED ?= $(shell command -v gsed)
endif
ifeq ($(shell ${SED} --version 2>&1 | grep -q GNU; echo $$?),1)
    $(error !!! GNU sed is required. If on OS X, use 'brew install gnu-sed'.)
endif

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) \
		rbac:roleName=manager-role output:rbac:artifacts:config=config/components/rbac\
		crd:generateEmbeddedObjectMeta=true output:crd:artifacts:config=config/components/crd/bases\
		paths="./api/..."
	$(CONTROLLER_GEN) \
		rbac:roleName=manager-role output:rbac:artifacts:config=config/components/rbac\
		webhook output:webhook:artifacts:config=config/components/webhook\
		paths="./pkg/..."
	cp -f ./config/components/crd/bases/jobset.x-k8s.io_jobsets.yaml ./charts/jobset/crds/

.PHONY: generate
generate: manifests controller-gen code-generator openapi-gen helm helm-docs ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations and client-go libraries.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
	./hack/update-codegen.sh $(GO_CMD) $(PROJECT_DIR)/bin
	./hack/python-sdk/gen-sdk.sh

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(GO_CMD) fmt ./...

.PHONY: fmt-verify
fmt-verify:
	@out=`$(GO_FMT) -w -l -d $$(find . -name '*.go')`; \
	if [ -n "$$out" ]; then \
	    echo "$$out"; \
	    exit 1; \
	fi

.PHONY: toc-update
toc-update:
	./hack/update-toc.sh

.PHONY: toc-verify
toc-verify:
	./hack/verify-toc.sh

.PHONY: helm-verify
helm-verify: helm-unittest helm-lint
	${HELM} template charts/jobset


.PHONY: vet
vet: ## Run go vet against code.
	$(GO_CMD) vet ./...

.PHONY: ci-lint
ci-lint: golangci-lint 
	$(GOLANGCI_LINT) run --timeout 15m0s

.PHONY: lint-api
lint-api: golangci-lint-kal
	$(GOLANGCI_LINT_KAL) run -v --config $(PROJECT_DIR)/.golangci-kal.yml
	
.PHONY: lint-api-fix
lint-api-fix: golangci-lint-kal
	$(GOLANGCI_LINT_KAL) run -v --config $(PROJECT_DIR)/.golangci-kal.yml --fix

.PHONY: test
test: manifests fmt vet envtest gotestsum test-python-sdk
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" $(GOTESTSUM) --junitfile $(ARTIFACTS)/junit.xml -- ./pkg/... ./api/... -coverprofile  $(ARTIFACTS)/cover.out

.PHONY: test-python-sdk
test-python-sdk:
	echo "testing Python SDK..."
	./hack/python-sdk/test-sdk.sh

.PHONY: verify
verify: vet fmt-verify ci-lint lint-api manifests generate helm-verify toc-verify generate-apiref
	git --no-pager diff --exit-code config api client-go sdk charts


##@ Build
.PHONY: install-go-deps
install-go-deps:
	$(GO_BUILD_ENV) $(GO_CMD) mod download

.PHONY: build
build: install-go-deps manifests ## Build manager binary.
	$(GO_BUILD_ENV) $(GO_CMD) build -ldflags="$(LD_FLAGS)" -o bin/manager main.go

.PHONY: run
run: install-go-deps manifests fmt vet ## Run a controller from your host.
	$(GO_CMD) run ./main.go

# Build the container image
.PHONY: image-local-build
image-local-build:
	BUILDER=$(shell $(DOCKER_BUILDX_CMD) create --use)
	$(MAKE) image-build PUSH=$(PUSH)
	$(DOCKER_BUILDX_CMD) rm $$BUILDER

.PHONY: image-local-push
image-local-push: PUSH=--push
image-local-push: image-local-build

.PHONY: image-build
image-build:
	$(IMAGE_BUILD_CMD) -t $(IMAGE_TAG) -t $(IMAGE_REPO):$(BRANCH_NAME) \
		--platform=$(PLATFORMS) \
		--build-arg BASE_IMAGE=$(BASE_IMAGE) \
		--build-arg BUILDER_IMAGE=$(BUILDER_IMAGE) \
		--build-arg CGO_ENABLED=$(CGO_ENABLED) \
		$(PUSH) \
		$(IMAGE_BUILD_EXTRA_OPTS) ./

.PHONY: image-push
image-push: PUSH=--push
image-push: image-build

# Build the in-place restart agent binary (sidecar container mode)
.PHONY: in-place-restart-agent-build
in-place-restart-agent-build: install-go-deps
	$(GO_BUILD_ENV) $(GO_CMD) build -ldflags="$(LD_FLAGS)" -o bin/in-place-restart-agent cmd/in-place-restart-agent/main.go

# Build the in-place restart agent image (sidecar container mode)
.PHONY: in-place-restart-agent-image-build
in-place-restart-agent-image-build:
	$(IMAGE_BUILD_CMD) \
		-t $(IN_PLACE_RESTART_AGENT_IMAGE_TAG) \
		-t $(IN_PLACE_RESTART_AGENT_IMAGE_REPO):$(BRANCH_NAME) \
		-f $(IN_PLACE_RESTART_AGENT_DOCKERFILE) \
		--platform=$(PLATFORMS) \
		--build-arg BASE_IMAGE=$(BASE_IMAGE) \
		--build-arg BUILDER_IMAGE=$(BUILDER_IMAGE) \
		--build-arg CGO_ENABLED=$(CGO_ENABLED) \
		$(PUSH) \
		$(IMAGE_BUILD_EXTRA_OPTS) ./

.PHONY: in-place-restart-agent-image-push
in-place-restart-agent-image-push: PUSH=--push
in-place-restart-agent-image-push: in-place-restart-agent-image-build

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/components/crd | kubectl apply --server-side -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/components/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/components/manager && $(KUSTOMIZE) edit set image controller=${IMAGE_TAG}
	$(KUSTOMIZE) build config/default | kubectl apply --server-side -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Helm
.PHONY: helm-unittest
helm-unittest: helm-unittest-plugin ## Run Helm chart unittests.
	$(HELM) unittest $(JOBSET_CHART_DIR) --strict --file "tests/**/*_test.yaml"

.PHONY: helm-lint
helm-lint: ## Run Helm chart lint test.
	${HELM} lint charts/jobset

.PHONY: helm-docs
helm-docs: helm-docs-plugin ## Generates markdown documentation for helm charts from requirements and values files.
	$(HELM_DOCS) --sort-values-order=file

.PHONY: helm-chart-package
helm-chart-package: yq helm ## Package a chart into a versioned chart archive file.
	DEST_CHART_DIR=$(DEST_CHART_DIR) \
	HELM="$(HELM)" YQ="$(YQ)" GIT_TAG="$(GIT_TAG)" IMAGE_REGISTRY="$(IMAGE_REGISTRY)" \
	HELM_CHART_PUSH=$(HELM_CHART_PUSH) \
	./hack/helm-chart-package.sh

.PHONY: helm-chart-push
helm-chart-push: HELM_CHART_PUSH=true
helm-chart-push: helm-chart-package

##@ Release

# Chart version should not have "v".
.PHONY: prepare-release-branch
prepare-release-branch: kustomize ## Prepare the release branch with the release version.
	cd config/components/manager && $(KUSTOMIZE) edit set image controller=${IMAGE_REPO}:${VERSION}
	$(SED) -r "s|download/v[0-9]+\.[0-9]+\.[0-9]+|download/${VERSION}|g" -i README.md
	$(SED) -r "s/v[0-9]+\.[0-9]+\.[0-9]+/${VERSION}/g" -i site/hugo.toml
	$(SED) -r "s/[0-9]+\.[0-9]+\.[0-9]+/$(shell echo ${VERSION} | sed 's/^v//')/g" -i charts/jobset/Chart.yaml
	make helm-docs

.PHONY: clean-artifacts
clean-artifacts: 
	if [ -d artifacts ]; then rm -rf artifacts; fi
	mkdir -p artifacts

.PHONY: artifacts
artifacts: clean-artifacts kustomize helm yq helm-chart-package ## Generate release artifacts.
	$(KUSTOMIZE) build config/default -o artifacts/manifests.yaml
	$(KUSTOMIZE) build config/prometheus -o artifacts/prometheus.yaml
	@$(call clean-manifests)

GOLANGCI_LINT = $(PROJECT_DIR)/bin/golangci-lint
GOLANGCI_LINT_KAL = $(PROJECT_DIR)/bin/golangci-lint-kube-api-linter
.PHONY: golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v2.7.2

.PHONY: golangci-lint-kal
golangci-lint-kal: golangci-lint ## Build golangci-lint-kal from custom configuration.
	cd $(PROJECT_DIR)/hack/golangci-kal; GOTOOLCHAIN=go1.25.0 $(GOLANGCI_LINT) custom; mv bin/golangci-lint-kube-api-linter $(PROJECT_DIR)/bin/

GOTESTSUM = $(shell pwd)/bin/gotestsum
.PHONY: gotestsum
gotestsum: ## Download gotestsum locally if necessary.
	@GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install gotest.tools/gotestsum@v1.8.2


.PHONY: generate-apiref
generate-apiref: genref
	cd $(PROJECT_DIR)/hack/genref/ && $(GENREF) -o $(PROJECT_DIR)/site/content/en/docs/reference

GENREF = $(PROJECT_DIR)/bin/genref
.PHONY: genref
genref: ## Download genref locally if necessary.
	@GOBIN=$(PROJECT_DIR)/bin $(GO_CMD) install github.com/kubernetes-sigs/reference-docs/genref@v0.28.0

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.2.1
CONTROLLER_TOOLS_VERSION ?= v0.17.2
# ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script.
ENVTEST_VERSION ?= $(shell $(GO_CMD) list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
# ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries.
ENVTEST_K8S_VERSION ?= $(shell $(GO_CMD) list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
HELM_VERSION ?= v3.17.1
HELM_UNITTEST_VERSION ?= 0.7.2
HELM_DOCS_VERSION ?= v1.14.2

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
HELM ?= $(ARTIFACTS)/helm
HELM_DOCS ?= $(ARTIFACTS)/helm-docs

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) $(GO_CMD) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)


# Use same code-generator version as k8s.io/api
CODEGEN_VERSION := $(shell $(GO_CMD) list -m -f '{{.Version}}' k8s.io/api)
CODEGEN = $(shell pwd)/bin/code-generator
CODEGEN_ROOT = $(shell $(GO_CMD) env GOMODCACHE)/k8s.io/code-generator@$(CODEGEN_VERSION)
.PHONY: code-generator
code-generator:
	@GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install k8s.io/code-generator/cmd/client-gen@$(CODEGEN_VERSION)
	cp -f $(CODEGEN_ROOT)/generate-groups.sh $(PROJECT_DIR)/bin/
	cp -f $(CODEGEN_ROOT)/generate-internal-groups.sh $(PROJECT_DIR)/bin/
	cp -f $(CODEGEN_ROOT)/kube_codegen.sh $(PROJECT_DIR)/bin/


.PHONY: openapi-gen
openapi-gen:
	@GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install k8s.io/kube-openapi/cmd/openapi-gen@latest
	$(PROJECT_DIR)/bin/openapi-gen --go-header-file hack/boilerplate.go.txt --output-dir api/jobset/v1alpha2 --output-pkg api/jobset/v1alpha2 --output-file openapi_generated.go --alsologtostderr ./api/jobset/v1alpha2

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) $(GO_CMD) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

GINKGO = $(shell pwd)/bin/ginkgo
.PHONY: ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	@GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install github.com/onsi/ginkgo/v2/ginkgo@v2.1.4

KIND = $(shell pwd)/bin/kind
.PHONY: kind
kind:
	@GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install sigs.k8s.io/kind@v0.29.0

.PHONY: kind-image-build
kind-image-build: PLATFORMS=linux/amd64
kind-image-build: IMAGE_BUILD_EXTRA_OPTS=--load
kind-image-build: kind image-build

.PHONY: test-integration
test-integration: manifests fmt vet envtest ginkgo ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
	$(GINKGO) --junit-report=junit.xml --output-dir=$(ARTIFACTS) $(GINKGO_ARGS) -v $(INTEGRATION_TARGET)

.PHONY: test-e2e-kind
test-e2e-kind: manifests kustomize fmt vet envtest ginkgo kind-image-build
	E2E_KIND_VERSION=$(E2E_KIND_VERSION) KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) USE_EXISTING_CLUSTER=$(USE_EXISTING_CLUSTER) ARTIFACTS=$(ARTIFACTS) IMAGE_TAG=$(IMAGE_TAG) E2E_TARGET_FOLDER=$(E2E_TARGET_FOLDER) ./hack/e2e-test.sh

.PHONY: prometheus
prometheus:
	kubectl apply --server-side -k config/prometheus

HELM = $(PROJECT_DIR)/bin/helm
.PHONY: helm
helm: ## Download helm locally if necessary.
	GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install helm.sh/helm/v3/cmd/helm@$(HELM_VERSION)

.PHONY: helm-unittest-plugin
helm-unittest-plugin: helm ## Download helm unittest plugin locally if necessary.
	if [ -z "$(shell $(HELM) plugin list | grep unittest)" ]; then \
		echo "Installing helm unittest plugin"; \
		$(HELM) plugin install https://github.com/helm-unittest/helm-unittest.git --version $(HELM_UNITTEST_VERSION); \
	fi

HELM_DOCS= $(PROJECT_DIR)/bin/helm-docs
.PHONY: helm-docs-plugin
helm-docs-plugin:
	GOBIN=$(LOCALBIN) $(GO_CMD) install github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)

YQ = $(PROJECT_DIR)/bin/yq
.PHONY: yq
yq: ## Download yq locally if necessary.
	GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on $(GO_CMD) install github.com/mikefarah/yq/v4@v4.45.1

## Docs website development
.PHONY: site-install-npm-dependencies
site-install-npm-dependencies:
	cd $(PROJECT_DIR)/site && npm install

HUGO_VERSION ?= 0.148.1
HUGO_CMD = $(PROJECT_DIR)/bin/hugo
.PHONY: site-install-hugo
site-install-hugo:
	GOBIN=$(PROJECT_DIR)/bin GO111MODULE=on CGO_ENABLED=1 $(GO_CMD) install -tags extended github.com/gohugoio/hugo@v$(HUGO_VERSION)

.PHONY: site-serve
site-serve: site-install-hugo site-install-npm-dependencies
	cd $(PROJECT_DIR)/site && $(HUGO_CMD) serve -D

.PHONY: site-build
site-build: site-install-hugo site-install-npm-dependencies
	cd $(PROJECT_DIR)/site && $(HUGO_CMD) --gc --minify
