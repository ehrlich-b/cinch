.PHONY: build build-go test fmt lint clean web web-deps web-dev dev dev-worker run check fly-create fly-deploy fly-logs fly-tail fly-status fly-ssh

# Build the cinch binary (includes web assets)
build: web build-go

# Build just the Go binary (fast, for iteration)
build-go:
	go build -o bin/cinch ./cmd/cinch

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Lint code (auto-installs golangci-lint if missing)
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint
lint:
	@test -f $(GOLANGCI_LINT) || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GOLANGCI_LINT) run

# Full pre-commit check
check: fmt test lint

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
dev: build-go
	./bin/cinch server

# Run worker in dev mode
dev-worker: build-go
	./bin/cinch worker

# Run a command locally (usage: make run CMD="echo hello")
run: build-go
	./bin/cinch run $(CMD)

# Run bare metal (usage: make run-bare CMD="echo hello")
run-bare: build-go
	./bin/cinch run --bare-metal $(CMD)

# Validate config in current directory
validate: build-go
	./bin/cinch config validate

# Run cinch run using config file
ci: build-go
	./bin/cinch run

# -------- Fly.io Deployment --------
#
# First-time setup:
#   1. make fly-create   (create app, allocate IPs, volume)
#   2. make fly-deploy   (deploy)
#
# Subsequent deploys: make fly-deploy

FLY_APP := cinch

fly-create:
	@echo "Creating Fly.io application..."
	fly apps create $(FLY_APP)
	fly volumes create cinch_data --size 1 --region iad -a $(FLY_APP) -y
	@echo ""
	@echo "App created!"
	@echo "1. In Cloudflare: cinch.sh CNAME $(FLY_APP).fly.dev (proxied)"
	@echo "2. Set SSL mode: Full (Strict)"
	@echo "3. Run: make fly-deploy"

fly-deploy:
	@echo "Deploying to Fly.io..."
	fly deploy

fly-logs:
	fly logs -a $(FLY_APP) --no-tail

fly-tail:
	fly logs -a $(FLY_APP)

fly-status:
	@fly status -a $(FLY_APP)
	@echo ""
	@echo "IPs (point cinch.sh DNS here):"
	@fly ips list -a $(FLY_APP)

fly-ssh:
	fly ssh console -a $(FLY_APP)
