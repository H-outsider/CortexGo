GO ?= go
GOCACHE ?= /tmp/cortexgo-gocache
export GOCACHE

.PHONY: test race vet coverage bench build ci

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

coverage:
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out

bench:
	$(GO) test ./... -run '^$$' -bench=. -benchmem

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags='-s -w' -o bin/cortexgo ./cmd/cortexgo

ci: vet test race build
