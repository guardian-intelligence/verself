.PHONY: build clean test lint fmt vet tidy \
       doctor setup-sops edit-secrets setup-domain \
       server-profile deploy e2e benchmark

BINARY := bmci
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Nix server profile store path (cached after first build)
NIX_PROFILE = $(shell nix build .#server-profile --no-link --print-out-paths 2>/dev/null)

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/bmci

clean:
	rm -f $(BINARY)

test:
	go test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofumpt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

doctor: build ## Check that all required dev tools are present and at the right version
	./$(BINARY) doctor

setup-sops: ## Generate age key, encrypt initial secrets, install sops collection
	./scripts/setup-sops.sh

edit-secrets: ## Open encrypted secrets in $EDITOR via sops
	sops ansible/group_vars/all/secrets.sops.yml

setup-domain: ## Configure Cloudflare domain (interactive wizard)
	nix develop --command ./scripts/setup-domain.sh $(DOMAIN)

server-profile: ## Build Nix server profile (golden image closure)
	nix build .#server-profile --print-out-paths

deploy: ## Deploy to all nodes (idempotent, no wipe)
	cd ansible && ansible-playbook playbooks/dev-single-node.yml \
		-e nix_server_profile_path=$(NIX_PROFILE)

e2e: ## Full wipe + reprovision + test
	cd ansible && ansible-playbook playbooks/ci-e2e.yml \
		-e nix_server_profile_path=$(NIX_PROFILE)

benchmark: ## Benchmark wipe+reprovision (3 iterations)
	./scripts/benchmark-reprovision.sh 3 $(NIX_PROFILE)
