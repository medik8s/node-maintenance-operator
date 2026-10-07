# Build the manager binary
FROM quay.io/konveyor/builder:ubi9-latest AS builder
ARG TARGETOS
ARG TARGETARCH
ARG OPERATOR_VERSION=""

WORKDIR /workspace

# Copy the Go Modules manifests and committed release version.
COPY go.mod go.sum ./
COPY Makefile Makefile
ENV GOTOOLCHAIN=auto

# Copy the go source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY pkg/ pkg/
COPY hack/ hack/
COPY vendor/ vendor/
COPY version/ version/

# for getting version info
COPY .git/ .git/

RUN go version
RUN git config --global --add safe.directory /workspace
# Do not inherit the builder image's generic VERSION environment variable.
RUN VERSION="${OPERATOR_VERSION}" ./hack/build.sh

# Use ubi-micro as minimal base image to package the manager binary - https://catalog.redhat.com/software/containers/ubi9-micro/61832b36dd607bfc82e66399
FROM registry.access.redhat.com/ubi9/ubi-micro:latest
WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
