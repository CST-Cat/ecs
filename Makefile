GO ?= go
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X ecs/internal/buildinfo.Version=$(VERSION) \
	-X ecs/internal/buildinfo.Commit=$(COMMIT) \
	-X ecs/internal/buildinfo.BuildDate=$(BUILD_DATE)

.PHONY: build test check fmt clean cross

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/ecs ./cmd/ecs

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

cross:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_linux_amd64 ./cmd/ecs
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_linux_arm64 ./cmd/ecs
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_freebsd_amd64 ./cmd/ecs
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_darwin_amd64 ./cmd/ecs
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_darwin_arm64 ./cmd/ecs
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/ecs_windows_amd64.exe ./cmd/ecs

clean:
	rm -rf bin dist
