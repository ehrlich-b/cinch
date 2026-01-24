# Cinch Makefile - The canonical example for Go projects using Cinch CI
#
# This Makefile demonstrates Cinch's philosophy: your Makefile IS your CI pipeline.
# One command runs everything. Tag pushes automatically trigger releases.
#
# Environment variables provided by Cinch:
#   CINCH_REF      - Full git ref (refs/heads/main or refs/tags/v1.0.0)
#   CINCH_BRANCH   - Branch name (empty for tags)
#   CINCH_TAG      - Tag name (empty for branches)
#   CINCH_COMMIT   - Git commit SHA
#   CINCH_FORGE    - Forge type (github, gitlab, forgejo, gitea)
#   GITHUB_TOKEN   - Installation token for GitHub API (releases, etc.)

.PHONY: build test fmt lint check ci release clean web web-deps web-dev dev dev-worker run

# -----------------------------------------------------------------------------
# CI Entry Point - This is what Cinch runs
# -----------------------------------------------------------------------------

ci: check
	@if [ -n "$$CINCH_TAG" ]; then $(MAKE) release; fi

# -----------------------------------------------------------------------------
# Development
# -----------------------------------------------------------------------------

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

# Full check: format, test, lint
check: fmt test lint

# Clean build artifacts
clean:
	rm -rf bin/ dist/ web/dist/

# -----------------------------------------------------------------------------
# Web Frontend
# -----------------------------------------------------------------------------

web:
	cd web && npm run build

web-deps:
	cd web && npm install

web-dev:
	cd web && npm run dev

# -----------------------------------------------------------------------------
# Local Development
# -----------------------------------------------------------------------------

dev: build-go
	./bin/cinch server

dev-worker: build-go
	./bin/cinch worker

run: build-go
	./bin/cinch run $(CMD)

run-bare: build-go
	./bin/cinch run --bare-metal $(CMD)

validate: build-go
	./bin/cinch config validate

# -----------------------------------------------------------------------------
# Release - Triggered automatically when CINCH_TAG is set
# -----------------------------------------------------------------------------

VERSION := $(shell git describe --tags --always)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

release: web
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "Error: GITHUB_TOKEN not set (run via Cinch or set manually)"; \
		exit 1; \
	fi
	@echo "Building $(VERSION) for all platforms..."
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		output="dist/cinch-$$os-$$arch"; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags="$(LDFLAGS)" -o $$output ./cmd/cinch; \
	done
	@echo "Creating GitHub release $(VERSION)..."
	gh release create $(VERSION) dist/* --generate-notes
	@echo "Release complete: $(VERSION)"

# -----------------------------------------------------------------------------
# Fly.io Deployment (manual, not part of CI)
# -----------------------------------------------------------------------------

FLY_APP := cinch

fly-create:
	fly apps create $(FLY_APP)
	fly volumes create cinch_data --size 1 --region iad -a $(FLY_APP) -y

fly-deploy:
	fly deploy --no-cache

fly-logs:
	fly logs -a $(FLY_APP) --no-tail

fly-tail:
	fly logs -a $(FLY_APP)

fly-status:
	@fly status -a $(FLY_APP)
	@echo ""
	@fly ips list -a $(FLY_APP)

fly-ssh:
	fly ssh console -a $(FLY_APP)
