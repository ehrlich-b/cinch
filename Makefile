.PHONY: build test fmt lint clean web dev

# Build the cinch binary
build: web
	go build -o bin/cinch ./cmd/cinch

# Build without rebuilding web assets (for faster iteration)
build-go:
	go build -o bin/cinch ./cmd/cinch

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...
	gofumpt -w .

# Lint code
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf web/dist/

# Build web assets
web:
	cd web && npm run build

# Install web dependencies
web-deps:
	cd web && npm install

# Run web dev server (for frontend development)
web-dev:
	cd web && npm run dev

# Run server in dev mode
dev:
	go run ./cmd/cinch server

# Run worker in dev mode
dev-worker:
	go run ./cmd/cinch worker
