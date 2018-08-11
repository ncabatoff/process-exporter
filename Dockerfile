# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang:1.10 AS build

# Copy the local package files to the container's workspace.
ADD . /go/src/github.com/ncabatoff/process-exporter

WORKDIR /go/src/github.com/ncabatoff/process-exporter

RUN curl -L -s https://github.com/golang/dep/releases/download/v0.5.0/dep-linux-amd64 -o $GOPATH/bin/dep
RUN chmod +x $GOPATH/bin/dep
RUN dep ensure

# Build the process-exporter command inside the container.
RUN make -C /go/src/github.com/ncabatoff/process-exporter

FROM scratch

COPY --from=build /go/src/github.com/ncabatoff/process-exporter/process-exporter /bin/process-exporter

# Run the process-exporter command by default when the container starts.
ENTRYPOINT ["/bin/process-exporter"]

# Document that the service listens on port 9256.
EXPOSE 9256
