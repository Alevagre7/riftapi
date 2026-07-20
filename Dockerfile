# syntax=docker/dockerfile:1.6
# Multi-stage build. The final image is distroless, runs as nonroot, ~10MB on disk.
# Default target arch: linux/arm64 (the Pi 3B). Override with --platform=linux/amd64 for dev.

# ---------- build stage ----------
FROM golang:1.25 AS build
WORKDIR /src

# Cache the module download layer
COPY go.mod go.sum* ./
RUN go mod download

# Copy source and build both binaries
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=arm64
ENV CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}
RUN make build-api build-sync \
 && ls -la bin/

# ---------- runtime stage (api) ----------
FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /src/bin/riftapi /riftapi
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/riftapi"]

# ---------- runtime stage (sync) ----------
FROM gcr.io/distroless/static-debian12:nonroot AS sync
COPY --from=build /src/bin/riftapi-sync /riftapi-sync
USER nonroot:nonroot
ENTRYPOINT ["/riftapi-sync"]
