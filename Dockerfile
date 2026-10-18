# syntax=docker/dockerfile:1

# ---- web ------------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /src/web/v2

COPY web/v2/package.json web/v2/package-lock.json ./
RUN npm ci

COPY web/v2/ ./
RUN npm run build

# ---- build -----------------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /src/web/v2/dist ./web/v2/dist

ARG VERSION=dev
ARG BUILD=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/echoface/ozddns/internal/version.Version=${VERSION} \
        -X github.com/echoface/ozddns/internal/version.Build=${BUILD} \
        -X github.com/echoface/ozddns/internal/version.Commit=${COMMIT}" \
      -o /out/ozddns ./cmd/ozddns

# ---- runtime ---------------------------------------------------------------
# Alpine (not scratch/distroless) so the "shell command" IP source works and
# ca-certificates/tzdata are available for HTTPS providers and timezones.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl \
 && adduser -D -u 10001 ozddns \
 && mkdir -p /data && chown -R ozddns:ozddns /data

COPY --from=build /out/ozddns /usr/local/bin/ozddns

# The bind address is deployment config, read from the environment (or
# config.yaml / --listen) — never from the database. The health check below
# resolves the same address, so it stays in sync if you change this.
ENV OZDDNS_LISTEN=0.0.0.0:9876

USER ozddns
VOLUME ["/data"]
# Run from the data volume so the auto-created config.yaml lands somewhere
# writable and persistent (default path ./config.yaml -> /data/config.yaml).
WORKDIR /data
EXPOSE 9876

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["ozddns", "health"]

ENTRYPOINT ["ozddns"]
CMD ["--db", "/data/ozddns.db", "serve"]
