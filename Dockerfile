# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go ./

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o ingress-migration-tool main.go

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary from builder
COPY --from=builder /build/ingress-migration-tool /app/ingress-migration-tool

# Run as non-root user (distroless nonroot user is 65532)
USER 65532:65532

ENTRYPOINT ["/app/ingress-migration-tool"]
