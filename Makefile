# riftapi — self-hosted read-only API for Riftbound card data
#
# Common targets:
#   make build       — compile both binaries into ./bin/
#   make test        — run the full test suite
#   make test PKG=./internal/scrape — run a single package's tests
#   make vet         — go vet ./...
#   make fmt         — format all Go packages
#   make tidy        — go mod tidy
#   make docker      — build the API image for linux/arm64 (the Pi target)
#   make clean       — remove ./bin and coverage files
#
# Cross-compilation note: GOOS/GOARCH env vars override the host for binary
# builds. Docker defaults to the Pi target; select the image stage with
# DOCKER_TARGET=api or DOCKER_TARGET=sync.

BIN_DIR    := bin
PKG_PREFIX := ./cmd/...
PKG        ?= ./...

GO         ?= go
GOOS       ?= $(shell $(GO) env GOOS)
GOARCH     ?= $(shell $(GO) env GOARCH)
CGO_ENABLED := 0

DOCKER_PLATFORM ?= linux/arm64
DOCKER_TARGET   ?= api

LDFLAGS    := -s -w
BUILD_TAGS := -tags 'osusergo,netgo'

.PHONY: build build-api build-sync test vet fmt tidy docker clean run-api run-sync

build: build-api build-sync

build-api:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(BUILD_TAGS) -ldflags '$(LDFLAGS)' \
		-o $(BIN_DIR)/riftapi ./cmd/riftapi

build-sync:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(BUILD_TAGS) -ldflags '$(LDFLAGS)' \
		-o $(BIN_DIR)/riftapi-scraper ./cmd/riftapi-scraper

test:
	$(GO) test -race -count=1 $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

docker:
	docker build --platform $(DOCKER_PLATFORM) --target $(DOCKER_TARGET) -t riftapi:latest .

clean:
	rm -rf $(BIN_DIR) coverage.txt coverage.html

# Convenience targets for local dev on the host (no Docker).
run-api: build-api
	./$(BIN_DIR)/riftapi

run-sync: build-sync
	./$(BIN_DIR)/riftapi-scraper
