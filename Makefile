# riftapi — self-hosted read-only API for Riftbound card data
#
# Common targets:
#   make build       — compile both binaries into ./bin/
#   make test        — run the full test suite
#   make test PKG=./internal/scrape — run a single package's tests
#   make vet         — go vet ./...
#   make fmt         — gofmt -w
#   make tidy        — go mod tidy
#   make docker      — build the API image for linux/arm64 (the Pi target)
#   make clean       — remove ./bin and coverage files
#
# Cross-compilation note: GOOS/GOARCH env vars override the host. The default
# docker target is linux/arm64 to match the Pi 3B. To produce an amd64 image
# (e.g. for testing on the dev machine), run `make docker GOARCH=amd64`.

BIN_DIR    := bin
PKG_PREFIX := ./cmd/...
PKG        ?= ./...

GO         ?= go
GOOS       ?= linux
GOARCH     ?= arm64
CGO_ENABLED := 0

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
		-o $(BIN_DIR)/riftapi-sync ./cmd/riftapi-sync

test:
	$(GO) test -race -count=1 $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

docker:
	docker build --platform $(GOOS)/$(GOARCH) -t riftapi:latest .

clean:
	rm -rf $(BIN_DIR) coverage.txt coverage.html

# Convenience targets for local dev on the host (no Docker).
run-api: build-api
	./$(BIN_DIR)/riftapi

run-sync: build-sync
	./$(BIN_DIR)/riftapi-sync
