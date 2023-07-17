# Build the manager binary
FROM quay.io/centos/centos:stream9 AS builder
RUN dnf install git-2.40.1 golang-1.20 -y && dnf clean all -y

# Ensurec orrect Go version
ENV GO_VERSION=1.20
RUN go install golang.org/dl/go${GO_VERSION}@latest \
    && ~/go/bin/go${GO_VERSION} download \
    && /bin/cp -f ~/go/bin/go${GO_VERSION} /usr/bin/go \
    && go version

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY api/ api/
COPY controllers/ controllers/
COPY hack/ hack/
COPY main.go main.go
COPY vendor/ vendor/
COPY version/ version/

# for getting version info
COPY .git/ .git/

# Build
RUN ./hack/build.sh

# Use ubi-micro as minimal base image to package the manager binary - https://catalog.redhat.com/software/containers/ubi8/ubi-micro/5ff3f50a831939b08d1b832a
FROM registry.access.redhat.com/ubi8/ubi-micro:8.8-1
WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
