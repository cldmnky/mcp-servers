GO ?= go

.DEFAULT_GOAL := build
.PHONY: build build-issues-mcp build-mcp format test vet clean run-mcp run-mcp-http

build: build-issues-mcp build-mcp

build-issues-mcp:
	mkdir -p bin
	$(GO) build -o bin/rh-issues-mcp ./pkg/mcp/rh-issues-mcp

build-mcp:
	mkdir -p bin
	$(GO) build -o bin/rhkcs-mcp ./pkg/mcp/rhkcs-mcp

format:
	$(GO) fmt ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

run-mcp: build-mcp
	./bin/rhkcs-mcp

run-mcp-http: build-mcp
	./bin/rhkcs-mcp -http :8080

clean:
	rm -f bin/rh-issues-mcp bin/rhkcs-mcp
