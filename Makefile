PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go)

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

VERSION ?= $(shell git describe --tags)
TAG ?= "minio/radio:$(VERSION)"

BUILD_LDFLAGS := '$(LDFLAGS)'

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: vet fmt lint

vet:
	@echo "Running $@ check"
	@GO111MODULE=on go vet github.com/minio/radio/...

fmt:
	@echo "Running $@ check"
	@GO111MODULE=on gofmt -d cmd/
	@GO111MODULE=on gofmt -d pkg/

lint:
	@echo "Running $@ check"
	@mkdir -p ${GOPATH}/bin
	@curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.21.0
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run

# Builds minio, runs the verifiers then runs the tests.
check: test
test: verifiers build
	@echo "Running unit tests"
	@GO111MODULE=on CGO_ENABLED=0 go test -tags kqueue ./... 1>/dev/null

# Builds radio locally.
build: checks
	@echo "Building radio binary to './radio'"
	@GO111MODULE=on CGO_ENABLED=0 go build -tags kqueue --ldflags $(BUILD_LDFLAGS) -o $(PWD)/radio 1>/dev/null

docker: build
	@docker build -t $(TAG) . -f Dockerfile.dev

# Builds radio and installs it to $GOPATH/bin.
install: build
	@echo "Installing radio binary to '$(GOPATH)/bin/radio'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/radio $(GOPATH)/bin/radio
	@echo "Installation successful. To learn more, try \"radio --help\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf radio
	@rm -rvf build
	@rm -rvf release
