# -------- build stage --------
FROM --platform=linux/amd64 golang:1.25-alpine AS builder
WORKDIR /src

# Install Node.js for web build
RUN apk add --no-cache nodejs npm

# Cache Go dependencies
COPY go.mod go.sum ./
RUN go mod download

# Cache web dependencies
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci

# Copy source and build
COPY . .
RUN cd web && npm run build
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /cinch ./cmd/cinch

# -------- runtime stage --------
FROM --platform=linux/amd64 alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates git sqlite
COPY --from=builder /cinch .

EXPOSE 8080
ENTRYPOINT ["/app/cinch", "server"]
