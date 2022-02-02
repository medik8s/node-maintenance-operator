# Build the manager binary
FROM quay.io/centos/centos:stream9 AS builder
RUN dnf install git golang -y

# Ensure go 1.16
RUN go install golang.org/dl/go1.16@latest
RUN ~/go/bin/go1.16 download
RUN /bin/cp -f ~/go/bin/go1.16 /usr/bin/go
RUN go version

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY main.go main.go
COPY api/ api/
COPY hack/ hack/
COPY controllers/ controllers/
COPY version/ version/
COPY vendor/ vendor/

# for getting version info
COPY .git/ .git/


# Build
RUN ./hack/build.sh

# Use ubi8 as minimal base image to package the manager binary
FROM registry.access.redhat.com/ubi8/ubi-minimal
WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532


ENTRYPOINT ["/manager"]
