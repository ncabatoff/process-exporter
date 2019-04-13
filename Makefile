pkgs          = $(shell go list ./... | grep -v /vendor/)

PREFIX                  ?= $(shell pwd)
BIN_DIR                 ?= $(shell pwd)
DOCKER_IMAGE_NAME       ?= ncabatoff/process-exporter
DOCKER_IMAGE_TAG        ?= $(subst /,-,$(shell git rev-parse --abbrev-ref HEAD))
SMOKE_TEST = -config.path packaging/conf/all.yaml -once-to-stdout-delay 1s |grep -q 'namedprocess_namegroup_memory_bytes{groupname="process-exporte",memtype="virtual"}'

all: format vet test build smoke

style:
	@echo ">> checking code style"
	@! gofmt -d $(shell find . -path ./vendor -prune -o -name '*.go' -print) | grep '^'

test:
	@echo ">> running short tests"
	go test -short $(pkgs)

format:
	@echo ">> formatting code"
	go fmt $(pkgs)

vet:
	@echo ">> vetting code"
	go vet $(pkgs)

build:
	@echo ">> building code"
	cd cmd/process-exporter; CGO_ENABLED=0 go build -o ../../process-exporter -a -tags netgo

smoke:
	@echo ">> smoke testing process-exporter"
	./process-exporter $(SMOKE_TEST)

integ:
	@echo ">> integration testing process-exporter"
	go build -o integration-tester cmd/integration-tester/main.go
	go build -o load-generator cmd/load-generator/main.go
	./integration-tester -write-size-bytes 65536

install:
	@echo ">> installing binary"
	cd cmd/process-exporter; CGO_ENABLED=0 go install -a -tags netgo

docker:
	@echo ">> building docker image"
	docker build -t "$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)" .
	docker rm configs
	docker create -v /packaging --name configs alpine:3.4 /bin/true
	docker cp packaging/conf configs:/packaging/conf
	docker run --rm --volumes-from configs "$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)" $(SMOKE_TEST)

dockertest:
	docker run --rm -it -v `pwd`:/go/src/github.com/ncabatoff/process-exporter golang:1.10  make -C /go/src/github.com/ncabatoff/process-exporter test

dockerinteg:
	docker run --rm -it -v `pwd`:/go/src/github.com/ncabatoff/process-exporter golang:1.10  make -C /go/src/github.com/ncabatoff/process-exporter build integ

.PHONY: all style format test vet build integ docker
