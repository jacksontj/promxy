BUILD := build
GO ?= go
GOFILES := $(shell find . -name "*.go" -type f ! -path "./vendor/*")
GOFMT ?= gofmt -s

.PHONY: clean
clean:
	$(GO) clean -i ./...
	rm -rf $(EXECUTABLE) $(DIST) $(BINDATA)

.PHONY: fmt
fmt:
	$(GOFMT) -w $(GOFILES)

.PHONY: test
test:
	$(GO) test ./...

.PHONY: release
release:
	./build.bash github.com/jacksontj/promxy/cmd/promxy $(BUILD)
