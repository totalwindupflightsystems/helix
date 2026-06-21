# Multi-stage build for Helix platform
# Stage 1: Build all Go binaries
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /helix-identity ./cmd/helix-identity/ && \
    CGO_ENABLED=0 go build -o /helix-estimate ./cmd/helix-estimate/ && \
    CGO_ENABLED=0 go build -o /helix-negotiate ./cmd/helix-negotiate/ && \
    CGO_ENABLED=0 go build -o /helix-prompt ./cmd/helix-prompt/ && \
    CGO_ENABLED=0 go build -o /helix-marketplace ./cmd/helix-marketplace/ && \
    CGO_ENABLED=0 go build -o /sandbox ./cmd/sandbox/

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates bubblewrap git curl

COPY --from=builder /helix-identity /usr/local/bin/helix-identity
COPY --from=builder /helix-estimate /usr/local/bin/helix-estimate
COPY --from=builder /helix-negotiate /usr/local/bin/helix-negotiate
COPY --from=builder /helix-prompt /usr/local/bin/helix-prompt
COPY --from=builder /helix-marketplace /usr/local/bin/helix-marketplace
COPY --from=builder /sandbox /usr/local/bin/sandbox

ENTRYPOINT ["/usr/local/bin/helix-identity"]
