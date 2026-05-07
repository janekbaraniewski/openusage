# syntax=docker/dockerfile:1

# ── builder ──────────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# CGO is required for mattn/go-sqlite3 (Cursor provider + telemetry store).
RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT_HASH=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-s -w \
      -X 'github.com/janekbaraniewski/openusage/internal/version.Version=${VERSION}' \
      -X 'github.com/janekbaraniewski/openusage/internal/version.CommitHash=${COMMIT_HASH}' \
      -X 'github.com/janekbaraniewski/openusage/internal/version.BuildDate=${BUILD_DATE}'" \
    -o /openusage ./cmd/openusage

# ── runtime ───────────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: needed for HTTPS calls to provider APIs from worker mode.
RUN apk add --no-cache ca-certificates

COPY --from=builder /openusage /usr/local/bin/openusage

EXPOSE 9190

ENTRYPOINT ["openusage", "hub", "--headless"]
