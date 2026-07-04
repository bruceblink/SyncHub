FROM golang:1.26-alpine AS build

ARG GOPROXY=https://goproxy.cn,direct

ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/bruceblink/SyncHub/internal/version.Version=${VERSION}" -o /out/synchub-api ./cmd/synchub-api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/bruceblink/SyncHub/internal/version.Version=${VERSION}" -o /out/synchub-cli ./cmd/synchub-cli
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/bruceblink/SyncHub/internal/version.Version=${VERSION}" -o /out/synchub-agent ./cmd/synchub-agent

FROM alpine:3.22

RUN addgroup -S synchub && adduser -S -G synchub synchub

WORKDIR /app

COPY --from=build /out/synchub-api /usr/local/bin/synchub-api
COPY --from=build /out/synchub-cli /usr/local/bin/synchub-cli
COPY --from=build /out/synchub-agent /usr/local/bin/synchub-agent

RUN mkdir -p /data/storage && chown -R synchub:synchub /data

USER synchub

ENV HTTP_ADDR=:8765 \
    DATABASE_DRIVER=sqlite \
    DATABASE_URL=/data/synchub.db \
    LOCAL_STORAGE_ROOT=/data/storage \
    VERSION_CLEANUP_INTERVAL_SECONDS=3600 \
    VERSION_RETENTION_MIN_VERSIONS=20 \
    VERSION_RETENTION_MAX_AGE_DAYS=30

EXPOSE 8765

VOLUME ["/data"]

ENTRYPOINT ["synchub-api"]
