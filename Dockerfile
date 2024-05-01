# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM --platform=$BUILDPLATFORM golang:1.22 AS build
ARG TARGETARCH
ARG BUILDPLATFORM
WORKDIR /go/src/github.com/ncabatoff/process-exporter
ADD . .

# Build the process-exporter command inside the container.
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH make build

FROM scratch

COPY --from=build /go/src/github.com/ncabatoff/process-exporter/process-exporter /bin/process-exporter

# Run the process-exporter command by default when the container starts.
ENTRYPOINT ["/bin/process-exporter"]

# Document that the service listens on port 9256.
EXPOSE 9256
