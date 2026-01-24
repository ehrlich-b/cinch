FROM golang:1.24-bookworm

# Install Node.js 22 LTS
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs

# Install Docker CLI and tools
RUN apt-get update && apt-get install -y \
    docker.io \
    make \
    git \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Trust mounted workspace (git safe.directory check fails on mounted volumes)
RUN git config --global --add safe.directory /workspace

# Set up Go environment
ENV GOPATH=/go
ENV PATH=$GOPATH/bin:$PATH

WORKDIR /workspace
