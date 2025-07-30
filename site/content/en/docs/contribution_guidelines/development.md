---
title: "Development Environment Setup"
linkTitle: "Dev Env Setup"
weight: 9
description: >
  How to set up a development environment to contribute to JobSet
---
## Dependencies
- `go>=1.24.0`
- `make`
- `kubectl`
- `git`
- `docker`
- Kubernetes cluster running one of the last 3 Kubernetes minor versions
  - [kind](https://kind.sigs.k8s.io/) allows you to run a local Kubernetes cluster using Docker containers

## Building and deploying JobSet from source

### Building the image
See [`Makefile`](https://github.com/kubernetes-sigs/jobset/blob/main/Makefile) targets for more information.
In particular:
- `make image-build`: Builds a JobSet image locally
- `make image-push`: Builds a JobSet image locally AND pushes it to the registry

The `make image-push` hook will attempt to push the built image to the public `us-central1-docker.pkg.dev/k8s-staging-images/jobset`
registry with an image tag determined by `git describe`. It is recommended to set your own `GIT_TAG` and `IMAGE_REGISTRY` environment
variables to ensure that your latest changes are pushed to an image registry your cluster can access.

One way to do this is by creating a `.env` file in the root of the repository:
```sh
export GIT_TAG="dev-$(date +%Y-%m-%d-%H%M%S)"
export IMAGE_REGISTRY=<YOUR_IMAGE_REGISTRY_URL>
```
Simply run `source .env` before running `make image-push` to ensure your image gets pushed to the correct registry with a unique tag.

### Deploying to a cluster
Once you've pushed an image, run `make undeploy deploy` to first remove any existing instances of JobSet on the cluster and then
deploy a new instance.

> NOTE: Make sure the `GIT_TAG` and `IMAGE_REGISTRY` environment variables are the same as when you ran `make image-push`.

## Running tests
Run `make test` for unit tests, and `make test-integration` for integration tests.

## Building the documentation site
We always welcome contributions to our [documentation](/), which is built using [hugo](https://gohugo.io/) and
[docsy](https://www.docsy.dev/docs/get-started/). To build the site and serve it locally, simply run `make site-serve`
and then open the provided URL in your browser.
