# AGENTS.md

This file provides guidance to AI assistants (Claude, Gemini, etc).

## Project Overview

JobSet is a Kubernetes-native API for managing groups of Kubernetes Jobs as a unit, designed for distributed AI/ML training workloads (PyTorch, Jax, TensorFlow) and HPC applications (MPI). It is a Kubernetes SIG (Special Interest Group) project maintained under kubernetes-sigs.

## Common Development Commands

### Building and Testing

```bash
# Build the manager binary
make build

# Run unit tests
make test

# Run integration tests
make test-integration

# Run E2E tests on Kind cluster
make test-e2e-kind

# Run specific test (use Ginkgo focus)
GINKGO_ARGS="-focus='specific test pattern'" make test-integration

# Format code
make fmt

# Run linters
make ci-lint        # General linting
make lint-api       # API-specific linting with golangci-lint-kal
make lint-api-fix   # Auto-fix API linting issues

# Run vet
make vet

# Full verification (runs before commits)
make verify
```

### Code Generation

JobSet uses extensive code generation for Kubernetes clients and CRDs. After modifying API types or adding new fields:

```bash
# Generate all: manifests, DeepCopy methods, client-go libraries, Python SDK
make generate

# Generate only CRDs and RBAC manifests
make manifests

# Update API reference documentation
make generate-apiref
```

### Building and Deploying Images

```bash
# Build image for Kind (local testing)
make kind-image-build

# Build multi-platform image
make image-build

# Build and push image (requires registry access)
make image-push

# Set custom registry/tag (recommended for development):
export GIT_TAG="dev-$(date +%Y-%m-%d-%H%M%S)"
export IMAGE_REGISTRY=<YOUR_IMAGE_REGISTRY_URL>
make image-push
```

### Kubernetes Deployment

```bash
# Install CRDs only
make install

# Deploy controller to cluster
make deploy

# Redeploy after code changes
make undeploy deploy

# Uninstall CRDs
make uninstall
```

### Documentation

```bash
# Serve documentation site locally (Hugo + Docsy)
make site-serve

# Build documentation site
make site-build

# Update table of contents in documentation
make toc-update
```

### Helm

```bash
# Run Helm chart unit tests
make helm-unittest

# Lint Helm chart
make helm-lint

# Verify Helm chart
make helm-verify

# Package Helm chart
make helm-chart-package
```

## Architecture Overview

### Core Components

1. **JobSet Controller** (`pkg/controllers/jobset_controller.go`)
   - Main reconciliation loop managing JobSet lifecycle
   - Handles creation, deletion, and updates of child Jobs
   - Implements restart logic and failure recovery
   - Coordinates with child Jobs to determine overall JobSet status

2. **Pod Controller** (`pkg/controllers/pod_controller.go`)
   - Watches Pods created by JobSet-owned Jobs
   - Updates JobSet status based on Pod readiness
   - Supports startup sequencing and dependency management

3. **Webhooks** (`pkg/webhooks/`)
   - **Validating Webhook** (`jobset_webhook.go`): Validates JobSet specs before admission
   - **Pod Mutating Webhook** (`pod_mutating_webhook.go`): Injects environment variables and configuration into Pods
   - **Pod Admission Webhook** (`pod_admission_webhook.go`): Enforces exclusive placement constraints

4. **Policy Controllers**
   - **Failure Policy** (`pkg/controllers/failure_policy.go`): Determines when to restart JobSet on failures
   - **Success Policy** (`pkg/controllers/success_policy.go`): Determines when JobSet is complete
   - **Startup Policy** (`pkg/controllers/startup_policy.go`): Manages startup sequencing between ReplicatedJobs
   - **TTL After Finished** (`pkg/controllers/ttl_after_finished.go`): Cleanup of completed JobSets
   - **Depends On** (`pkg/controllers/depends_on.go`): Handles dependency relationships

### API Structure

- **API Version**: `jobset.x-k8s.io/v1alpha2` (`api/jobset/v1alpha2/`)
- **Main Types**:
  - `JobSet`: Top-level resource representing a group of Jobs
  - `ReplicatedJob`: Specification for creating multiple identical Jobs
  - Each ReplicatedJob can have different pod templates (e.g., leader, workers, parameter servers)

### Key Concepts

1. **ReplicatedJobs**: JobSet contains one or more ReplicatedJobs, each creating `replicas` number of Jobs
2. **Automatic Networking**: Uses IndexedJobs + headless services for stable pod hostnames and DNS records
3. **Restart Semantics**: On failure, entire JobSet is recreated (all child Jobs deleted and recreated)
4. **Exclusive Placement**: Annotation-based exclusive topology placement for performance isolation
5. **Startup Sequencing**: Leader-worker patterns where certain Jobs must be ready before others start
6. **Integration with Kueue**: Queue-based resource management for multi-tenancy

### Testing Structure

- **Unit Tests**: Co-located with source files (`*_test.go`)
  - Use `testify/assert` for assertions
  - Use `testify/require` for fatal assertions
- **Integration Tests**: `test/integration/` - uses envtest (fake API server)
- **E2E Tests**: `test/e2e/` - uses real Kubernetes clusters (Kind for CI)
  - Uses Ginkgo/Gomega frameworks

### Code Generation Tools

JobSet uses multiple code generators (all invoked via `make generate`):
- **controller-gen**: Generates CRDs, RBAC, and webhook configs from Go markers
- **client-gen**: Generates typed clientsets for jobset resources
- **openapi-gen**: Generates OpenAPI spec for API validation
- **Python SDK generator**: `hack/python-sdk/gen-sdk.sh` generates Python client library

## Important Development Notes

### E2E Test Environment Variables
- `KIND_CLUSTER_NAME`: Name of Kind cluster (default: "kind")
- `USE_EXISTING_CLUSTER`: Set to "true" to test against existing cluster instead of creating new Kind cluster
- `E2E_KIND_VERSION`: Kubernetes version for Kind cluster (default: kindest/node:v1.34.0)
- `E2E_TARGET`: Test target pattern (default: ./test/e2e/...)

### Working with APIs

When modifying JobSet API (`api/jobset/v1alpha2/jobset_types.go`):
1. Add kubebuilder markers for validation, defaulting, or CRD generation
2. Run `make generate` to update generated code
3. Run `make verify` to ensure all generated code is up to date
4. The `+kubebuilder` comments are critical for CRD generation

### Webhook Development

Webhooks run as part of the JobSet controller manager. When testing locally:
- Webhooks require valid TLS certificates
- Use `cert-controller` package for certificate management
- Webhook configurations are in `config/components/webhook/`

### Metrics

JobSet exposes Prometheus metrics via `pkg/metrics/`. When adding new metrics, follow the existing pattern and register them in the metrics package init function.

### Important Constants

Check `pkg/constants/constants.go` for:
- Annotation keys (e.g., exclusive placement, coordinator labels)
- Label keys (e.g., jobset name, replicated job name, job index)
- Environment variable names injected into pods

### Release Process

Releases follow a 2-3 month cycle:
1. Update version in relevant places using `make prepare-release-branch VERSION=vX.Y.Z`
2. Generate release artifacts with `make artifacts`
3. Release artifacts include manifests, Prometheus config, and Helm chart
