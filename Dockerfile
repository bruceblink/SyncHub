FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/synchub-api ./cmd/synchub-api

FROM alpine:3.22

RUN addgroup -S synchub && adduser -S -G synchub synchub

WORKDIR /app

COPY --from=build /out/synchub-api /usr/local/bin/synchub-api

RUN mkdir -p /data/storage && chown -R synchub:synchub /data

USER synchub

ENV HTTP_ADDR=:8765 \
    DATABASE_DRIVER=sqlite \
    DATABASE_URL=/data/synchub.db \
    LOCAL_STORAGE_ROOT=/data/storage

EXPOSE 8765

VOLUME ["/data"]

ENTRYPOINT ["synchub-api"]
