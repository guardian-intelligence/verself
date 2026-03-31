.PHONY: build clean test test-integration lint fmt vet tidy \
       doctor setup-sops edit-secrets setup-domain \
       server-profile provision deprovision deploy e2e \
       guest-rootfs deploy-ci-artifacts

BINARY   := bmci
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

INVENTORY   := ansible/inventory/hosts.ini
REMOTE_HOST := $(shell grep -m1 'ansible_host=' $(INVENTORY) 2>/dev/null | sed 's/.*ansible_host=\([^ ]*\).*/\1/')
REMOTE_USER := $(shell grep -m1 'ansible_user=' $(INVENTORY) 2>/dev/null | sed 's/.*ansible_user=\([^ ]*\).*/\1/')
SSH_OPTS    := -o StrictHostKeyChecking=no

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

guest-rootfs: ## Build Alpine guest rootfs on the server
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	@test -n "$(REMOTE_HOST)" || { echo "ERROR: no ansible_host found in $(INVENTORY)"; exit 1; }
	@echo "→ building forgevm-init (static, linux/amd64)"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /tmp/forgevm-init ./cmd/forgevm-init
	@echo "→ uploading build script, version pins, and forgevm-init"
	scp $(SSH_OPTS) scripts/build-guest-rootfs.sh ci/versions.json /tmp/forgevm-init \
		$(REMOTE_USER)@$(REMOTE_HOST):/tmp/
	@echo "→ building guest rootfs on $(REMOTE_HOST)"
	ssh $(SSH_OPTS) -t $(REMOTE_USER)@$(REMOTE_HOST) \
		'cd /tmp && sudo env "PATH=$(REMOTE_PATH)" bash build-guest-rootfs.sh'

deploy-ci-artifacts: ## Deploy rootfs to /var/lib/ci/ on the server
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	@test -n "$(REMOTE_HOST)" || { echo "ERROR: no ansible_host found in $(INVENTORY)"; exit 1; }
	ssh $(SSH_OPTS) -t $(REMOTE_USER)@$(REMOTE_HOST) \
		'sudo cp /tmp/ci/output/rootfs.ext4 /var/lib/ci/rootfs.ext4'

# PATH for sudo on the remote — includes Nix profile where node lives.
REMOTE_PATH := /home/$(REMOTE_USER)/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
