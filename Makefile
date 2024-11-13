BUILD := build
GO ?= go
GOFILES := $(shell find . -name "*.go" -type f ! -path "./vendor/*")
GOFMT ?= gofmt
GOIMPORTS ?= goimports -local=github.com/jacksontj/promxy
STATICCHECK ?= staticcheck
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
GIT_REVISION := $(shell git rev-parse --short HEAD)
IMAGE_TAG ?= $(subst /,-,$(GIT_BRANCH))-$(GIT_REVISION)

.PHONY: clean
clean:
	$(GO) clean -i ./...
	rm -rf $(BUILD)

.PHONY: static-check
static-check:
	$(STATICCHECK) ./...

.PHONY: fmt
fmt:
	$(GOFMT) -w -s $(GOFILES)

.PHONY: imports
imports:
	$(GOIMPORTS) -w $(GOFILES)

.PHONY: test
test:
	GO111MODULE=on $(GO) test -race -mod=vendor -tags netgo,builtinassets ./...

.PHONY: release
release:
	./build.bash github.com/jacksontj/promxy/cmd/promxy $(BUILD)
	./build.bash github.com/jacksontj/promxy/cmd/remote_write_exporter $(BUILD)

.PHONY: build-image
build-image: clean
	docker buildx build --load --platform linux/amd64 --build-arg revision=$(GIT_REVISION) -t promxy:latest -t promxy:$(IMAGE_TAG) .

testlocal-build:
	docker build -t 127.0.0.1:32000/promxy:latest .
	docker push 127.0.0.1:32000/promxy:latest

.PHONY: vendor
vendor:
	GO111MODULE=on $(GO) mod tidy -compat=1.22
	GO111MODULE=on $(GO) mod vendor

.PHONY: update-prom-fork
update-prom-fork:
	GO111MODULE=on $(GO) mod edit -replace github.com/prometheus/prometheus=github.com/rishabhkumar92/prometheus@vrishabhk-promxy-upgrade
	$(MAKE) vendor
