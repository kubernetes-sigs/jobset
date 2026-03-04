# This is only for the sidecar container mode of the in-place restart agent
ARG BUILDER_IMAGE=golang:1.25
ARG BASE_IMAGE=gcr.io/distroless/static:nonroot

# Build the manager binary
FROM --platform=${BUILDPLATFORM} ${BUILDER_IMAGE} AS builder

ARG CGO_ENABLED
ARG TARGETARCH

WORKDIR /workspace

# Install dependencies
COPY go.mod go.sum Makefile ./
RUN make install-go-deps controller-gen

# Copy the go source
COPY . .

# Build
RUN make in-place-restart-agent-build GO_BUILD_ENV='CGO_ENABLED=${CGO_ENABLED} GOOS=linux GOARCH=${TARGETARCH}'

FROM --platform=${BUILDPLATFORM} ${BASE_IMAGE}
WORKDIR /
COPY --from=builder /workspace/bin/in-place-restart-agent .
USER 65532:65532

ENTRYPOINT ["/in-place-restart-agent"]
