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

getdeps:
	@mkdir -p ${GOPATH}/bin
	@which golint 1>/dev/null || (echo "Installing golint" && GO111MODULE=off go get -u golang.org/x/lint/golint)
ifeq ($(GOARCH),s390x)
	@which staticcheck 1>/dev/null || (echo "Installing staticcheck" && GO111MODULE=off go get honnef.co/go/tools/cmd/staticcheck)
else
	@which staticcheck 1>/dev/null || (echo "Installing staticcheck" && wget --quiet https://github.com/dominikh/go-tools/releases/download/2019.2.3/staticcheck_${GOOS}_${GOARCH}.tar.gz && tar xf staticcheck_${GOOS}_${GOARCH}.tar.gz && mv staticcheck/staticcheck ${GOPATH}/bin/staticcheck && chmod +x ${GOPATH}/bin/staticcheck && rm -f staticcheck_${GOOS}_${GOARCH}.tar.gz && rm -rf staticcheck)
endif
	@which misspell 1>/dev/null || (echo "Installing misspell" && GO111MODULE=off go get -u github.com/client9/misspell/cmd/misspell)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps vet fmt lint staticcheck spelling

vet:
	@echo "Running $@ check"
	@GO111MODULE=on go vet github.com/minio/radio/...

fmt:
	@echo "Running $@ check"
	@GO111MODULE=on gofmt -d cmd/
	@GO111MODULE=on gofmt -d pkg/

lint:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golint -set_exit_status github.com/minio/radio/cmd/...

staticcheck:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/staticcheck github.com/minio/radio/cmd/...
	@GO111MODULE=on ${GOPATH}/bin/staticcheck github.com/minio/radio/pkg/...

spelling:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/misspell -locale US -error `find cmd/`
	@GO111MODULE=on ${GOPATH}/bin/misspell -locale US -error `find pkg/`
	@GO111MODULE=on ${GOPATH}/bin/misspell -locale US -error `find buildscripts/`
	@GO111MODULE=on ${GOPATH}/bin/misspell -locale US -error `find dockerscripts/`

# Builds minio, runs the verifiers then runs the tests.
check: test
test: verifiers build
	@echo "Running unit tests"
	@GO111MODULE=on CGO_ENABLED=0 go test -tags kqueue ./... 1>/dev/null

# Verify radio binary
verify:
	@echo "Verifying build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -race -tags kqueue --ldflags $(BUILD_LDFLAGS) -o $(PWD)/radio 1>/dev/null
	@(env bash $(PWD)/buildscripts/verify-build.sh)

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
