# Cinch Makefile

.PHONY: build test fmt lint check release clean web web-deps web-dev dev dev-worker run push push-tags install-hooks

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

# Install git hooks (runs checks before commit)
install-hooks:
	git config --local core.hooksPath .githooks
	@echo "Git hooks installed. Pre-commit will run 'make check'."

# Clean build artifacts
clean:
	rm -rf bin/ dist/ web/dist/

# -----------------------------------------------------------------------------
# Web Frontend
# -----------------------------------------------------------------------------

web:
	cd web && npm install && npm run build

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
# Release (runs on tag push via Cinch CI)
# -----------------------------------------------------------------------------

VERSION := $(CINCH_TAG)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

release: web
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: CINCH_TAG not set (run via Cinch CI on tag push)"; \
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
	@echo "Creating release $(VERSION)..."
	cinch release dist/*

# -----------------------------------------------------------------------------
# Multi-Forge Push (GitHub, GitLab, Codeberg)
# -----------------------------------------------------------------------------

# Push main branch to all forges
push:
	@git push github main 2>/dev/null && echo "github pushed" || echo "github: skipped (remote not configured)"
	@git push gitlab main 2>/dev/null && echo "gitlab pushed" || echo "gitlab: skipped (remote not configured)"
	@git push codeberg main 2>/dev/null && echo "codeberg pushed" || echo "codeberg: skipped (remote not configured)"

# Push tags to all forges (triggers releases on all)
push-tags:
	@git push github --tags 2>/dev/null && echo "github tags pushed" || echo "github: skipped (remote not configured)"
	@git push gitlab --tags 2>/dev/null && echo "gitlab tags pushed" || echo "gitlab: skipped (remote not configured)"
	@git push codeberg --tags 2>/dev/null && echo "codeberg tags pushed" || echo "codeberg: skipped (remote not configured)"

# Tag and push everywhere
release-tag:
	@if [ -z "$(V)" ]; then echo "Usage: make release-tag V=v1.0.0"; exit 1; fi
	git tag $(V)
	$(MAKE) push
	$(MAKE) push-tags

# -----------------------------------------------------------------------------
# Fly.io Deployment (manual)
# -----------------------------------------------------------------------------

FLY_APP := cinch

fly-deploy:
	fly deploy

fly-logs:
	fly logs -a $(FLY_APP) --no-tail

fly-ssh:
	fly ssh console -a $(FLY_APP)
