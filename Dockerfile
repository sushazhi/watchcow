# WatchCow MVP Dockerfile
# Target: Debian 12 (bookworm) - matches fnOS
FROM golang:1.25-bookworm AS builder

# Configure Tsinghua mirror for faster builds in China
RUN sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources && \
    sed -i 's|security.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources

# Install eBPF build tools
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-amd64 \
    linux-libc-dev \
    make \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/include/x86_64-linux-gnu/asm /usr/include/asm

WORKDIR /build

# Copy go module files
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn go mod download

# Install bpf2go (match version in go.mod)
RUN GOPROXY=https://goproxy.cn go install github.com/cilium/ebpf/cmd/bpf2go@v0.20.0

# Copy source code
COPY . .

# Generate eBPF bindings and build
RUN go generate ./internal/ebpf && \
    go build -o watchcow ./cmd/watchcow

# Runtime image
FROM debian:bookworm-slim

# Configure Tsinghua mirror for faster builds in China
RUN sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources && \
    sed -i 's|security.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy binary from builder
COPY --from=builder /build/watchcow /usr/local/bin/watchcow

WORKDIR /app

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/watchcow"]
