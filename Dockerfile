# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Install git for fetching dependencies
RUN apk add --no-cache git

# Copy source code
COPY . .

# Tidy and download dependencies
RUN go mod tidy
RUN go mod download

# Build the application
# -o ezstremio: output binary name
# -ldflags="-w -s": strip debug info for smaller binary
RUN go build -ldflags="-w -s" -o ezstremio .

# Final stage
FROM alpine:latest

WORKDIR /app

# Install CA certificates for HTTPS requests and Chromium for scraping
RUN apk --no-cache add ca-certificates chromium

# Copy binary from builder
COPY --from=builder /app/ezstremio .

# Expose port (default 8080)
EXPOSE 8080

# Run the application
CMD ["./ezstremio"]
