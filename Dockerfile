# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /ghost-driver ./cmd/ghost-driver

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

# ffmpeg for GIF/MP4 rendering, xdotool only available on host display
RUN apk add --no-cache ffmpeg ca-certificates

COPY --from=builder /ghost-driver /usr/local/bin/ghost-driver

# stdio is the default transport (used by Claude Desktop / MCP clients)
ENTRYPOINT ["/usr/local/bin/ghost-driver", "--transport", "stdio"]
