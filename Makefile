BINARY    := nerve
PKG       := github.com/mascah/nerve
BUILD_DIR := bin
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X $(PKG)/internal/version.Version=$(VERSION)

.PHONY: all build install test lint clean fmt vet tidy run dev

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

# lint runs golangci-lint if available, otherwise falls back to go vet.
# Install golangci-lint: https://golangci-lint.run/usage/install/
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found; running go vet instead"; \
		go vet ./...; \
	fi

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BUILD_DIR) dist

run: build
	./$(BUILD_DIR)/$(BINARY) $(ARGS)

dev:
	go run ./cmd/$(BINARY) $(ARGS)
