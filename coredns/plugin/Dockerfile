# Build the manager binary
FROM mirror.gcr.io/library/golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

ARG GIT_SHA
ARG VERSION

ENV GIT_SHA=${GIT_SHA:-unknown}
ENV VERSION=${VERSION:-unknown}

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY dnsop/ dnsop/
COPY k8s.go k8s.go
COPY kuadrant.go kuadrant.go
COPY setup.go setup.go
COPY weighted.go weighted.go
COPY zone.go zone.go
COPY lookup.go lookup.go

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "-X main.pluginVersion=${VERSION} -X main.gitSHA=${GIT_SHA}" cmd/coredns.go

# Based on https://github.com/coredns/coredns/blob/master/Dockerfile
# Requires ths stage to avoid "Listen tcp :53: bind: permission denied" errors in nonroot containers.
FROM mirror.gcr.io/library/debian:stable-slim AS build
SHELL [ "/bin/sh", "-ec" ]

RUN export DEBCONF_NONINTERACTIVE_SEEN=true \
           DEBIAN_FRONTEND=noninteractive \
           DEBIAN_PRIORITY=critical \
           TERM=linux ; \
    apt-get -qq update ; \
    apt-get -yyqq upgrade ; \
    apt-get -yyqq install ca-certificates libcap2-bin; \
    apt-get clean
COPY --from=builder /workspace/coredns /coredns
RUN setcap cap_net_bind_service=+ep /coredns

# Use distroless as minimal base image to package the binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /coredns /coredns
#Copy the demo geo db
COPY geoip/GeoLite2-City-demo.mmdb /
USER nonroot:nonroot
WORKDIR /
EXPOSE 53 53/udp

ENTRYPOINT ["/coredns"]
