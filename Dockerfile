ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.22

FROM node:24-alpine AS admin-build

WORKDIR /src/web-admin

COPY web-admin/package.json web-admin/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

COPY web-admin/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build

ARG GOPROXY=https://goproxy.cn,direct
ARG TARGETOS=linux
ARG TARGETARCH=amd64

ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=admin-build /src/internal/api/admin_dist ./internal/api/admin_dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X github.com/bruceblink/SyncHub/internal/version.Version=${VERSION}" -o /out/synchub-api ./cmd/synchub-api

FROM ${RUNTIME_IMAGE}

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

LABEL org.opencontainers.image.title="SyncHub" \
      org.opencontainers.image.description="Developer workspace sync server" \
      org.opencontainers.image.source="https://github.com/bruceblink/SyncHub" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}"

RUN apk add --no-cache ca-certificates \
    && addgroup -S synchub \
    && adduser -S -G synchub synchub

WORKDIR /app

COPY --from=build /out/synchub-api /usr/local/bin/synchub-api

RUN mkdir -p /data/storage && chown -R synchub:synchub /data

USER synchub

ENV HTTP_ADDR=:8765 \
    DATABASE_DRIVER=postgres \
    STORAGE_BACKEND=local \
    LOCAL_STORAGE_ROOT=/data/storage \
    VERSION_CLEANUP_INTERVAL_SECONDS=3600 \
    VERSION_RETENTION_MIN_VERSIONS=20 \
    VERSION_RETENTION_MAX_AGE_DAYS=30

EXPOSE 8765

VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -qO- http://127.0.0.1:8765/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/synchub-api"]
