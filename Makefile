.PHONY: build clean test test-integration lint fmt vet tidy \
       doctor setup-sops edit-secrets setup-domain \
       server-profile provision deprovision deploy e2e benchmark

BINARY := bmci
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Nix server profile store path (cached after first build)
NIX_PROFILE = $(shell nix build .#server-profile --no-link --print-out-paths 2>/dev/null)

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/bmci

clean:
	rm -f $(BINARY)

test: ## Run unit tests
	go test -race ./...

test-integration: ## Run all tests including ZFS integration (requires sudo + zfs)
	@echo "Integration tests require root for ZFS pool operations."
	@echo "You may be prompted for your password."
	@echo ""
	sudo env PATH="$$PATH" go test -tags integration -race -count=1 ./...

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

setup-domain: build ## Configure Cloudflare domain (interactive wizard)
	./$(BINARY) setup-domain $(DOMAIN)

server-profile: ## Build Nix server profile (golden image closure)
	nix build .#server-profile --print-out-paths

provision: ## Provision bare metal via OpenTofu, generate Ansible inventory
	@test -f terraform/terraform.tfvars.json || \
		{ echo "Error: terraform/terraform.tfvars.json not found."; \
		  echo "Copy the example and fill in your project_id:"; \
		  echo "  cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json"; \
		  exit 1; }
	cd terraform && tofu init -input=false && tofu apply -var-file=terraform.tfvars.json
	./scripts/generate-inventory.sh

deprovision: ## Destroy all bare metal infrastructure
	cd terraform && tofu destroy -var-file=terraform.tfvars.json
	rm -f ansible/inventory/hosts.ini

deploy: ## Deploy to all nodes (idempotent, no wipe)
	cd ansible && ansible-playbook playbooks/dev-single-node.yml \
		-e nix_server_profile_path=$(NIX_PROFILE)

e2e: ## Full wipe + reprovision + test
	cd ansible && ansible-playbook playbooks/ci-e2e.yml \
		-e nix_server_profile_path=$(NIX_PROFILE)

benchmark: build ## Run CI benchmark workloads on ZFS clones
	sudo ./$(BINARY) benchmark
