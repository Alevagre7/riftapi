# syntax=docker/dockerfile:1.6
# Multi-stage build. The final image is distroless, runs as nonroot, ~10MB on disk.
# The image architecture follows Docker's selected build platform; use
# --platform=linux/arm64 for a Pi image.

# ---------- build stage ----------
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
WORKDIR /src

# Cache the module download layer
COPY go.mod go.sum* ./
RUN go mod download

# Copy source and build both binaries
COPY . .
RUN mkdir -p /src/data && touch /src/data/.keep
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}
RUN make build-api build-sync \
 && ls -la bin/

# ---------- runtime stage (api) ----------
FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /src/bin/riftapi /riftapi
COPY --from=build --chown=65532:65532 /src/data/ /data/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/riftapi"]

# ---------- runtime stage (sync) ----------
FROM gcr.io/distroless/static-debian12:nonroot AS sync
COPY --from=build /src/bin/riftapi-scraper /riftapi-scraper
COPY --from=build --chown=65532:65532 /src/data/ /data/
USER nonroot:nonroot
ENTRYPOINT ["/riftapi-scraper"]

# Keep a plain `docker build .` useful: the API is the long-running default
# image, while the one-shot scraper remains available via --target sync.
FROM api AS default
