.PHONY: build clean test test-integration lint lint-ansible fmt vet tidy \
	       hooks-install \
	       doctor setup-dev setup-sops edit-secrets setup-domain \
	       server-profile provision deprovision deploy deploy-dashboards \
	       clickhouse-shell clickhouse-query \
	       ci-fixtures-refresh ci-fixtures-run ci-fixtures-pass ci-fixtures-fail ci-fixtures-full \
	       guest-rootfs deploy-ci-artifacts smelter-build smelter-dev

BINARY   := forge-metal
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

INVENTORY   := ansible/inventory/hosts.ini
REMOTE_HOST := $(shell grep -m1 'ansible_host=' $(INVENTORY) 2>/dev/null | sed 's/.*ansible_host=\([^ ]*\).*/\1/')
REMOTE_USER := $(shell grep -m1 'ansible_user=' $(INVENTORY) 2>/dev/null | sed 's/.*ansible_user=\([^ ]*\).*/\1/')
SSH_OPTS    := -o StrictHostKeyChecking=no

# Nix server profile store path (cached after first build)
NIX_PROFILE = $(shell nix build .#server-profile --no-link --print-out-paths 2>/dev/null)

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/forge-metal

smelter-build: ## Build the homestead-smelter Zig host/guest binaries
	cd homestead-smelter && zig build -Doptimize=ReleaseSafe

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

lint-ansible:
	cd ansible && ansible-lint playbooks roles

hooks-install:
	@hooks_path=$$(git config --get core.hooksPath || true); \
	if [ "$$hooks_path" = "$(CURDIR)/.git/hooks" ] || [ "$$hooks_path" = ".git/hooks" ]; then \
		git config --unset-all core.hooksPath; \
	fi
	pre-commit install

fmt:
	gofumpt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

doctor: build ## Check that all required dev tools are present and at the right version
	./$(BINARY) doctor

setup-dev: ## Install pinned dev tools from dev-tools.json via Ansible
	cd ansible && ansible-playbook playbooks/setup-dev.yml

setup-sops: ## Generate age key, encrypt initial secrets, install sops collection
	cd ansible && ansible-playbook playbooks/setup-sops.yml

edit-secrets: ## Open encrypted secrets in $EDITOR via sops
	sops ansible/group_vars/all/secrets.sops.yml

setup-domain: build ## Configure Cloudflare domain (interactive wizard)
	./$(BINARY) setup-domain $(DOMAIN)

server-profile: ## Build Nix server profile (golden image closure)
	nix build .#server-profile --print-out-paths

SOPS_SECRETS = $(CURDIR)/ansible/group_vars/all/secrets.sops.yml
TOFU_ENV = LATITUDESH_AUTH_TOKEN=$$(sops -d --extract '["latitudesh_auth_token"]' $(SOPS_SECRETS))

provision: ## Provision bare metal via OpenTofu, generate Ansible inventory
	@test -f terraform/terraform.tfvars.json || \
		{ echo "Error: terraform/terraform.tfvars.json not found."; \
		  echo "Copy the example and fill in your project_id:"; \
		  echo "  cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json"; \
		  exit 1; }
	cd terraform && $(TOFU_ENV) tofu init -input=false && $(TOFU_ENV) tofu apply -auto-approve -var-file=terraform.tfvars.json
	./scripts/generate-inventory.sh

deprovision: ## Destroy all bare metal infrastructure
	cd terraform && $(TOFU_ENV) tofu destroy -auto-approve -var-file=terraform.tfvars.json
	rm -f ansible/inventory/hosts.ini

deploy: ## Deploy to all nodes (idempotent, no wipe)
	cd ansible && ansible-playbook playbooks/dev-single-node.yml \
		-e nix_server_profile_path=$(NIX_PROFILE)

deploy-dashboards: ## Sync HyperDX dashboards and sources without a full redeploy
	cd ansible && ansible-playbook playbooks/hyperdx-dashboards.yml

clickhouse-shell: ## Open an interactive clickhouse-client session on the worker
	./scripts/clickhouse.sh

clickhouse-query: ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=forge_metal]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

ci-fixtures-refresh: ## Rebuild and stage CI guest artifacts on the existing host
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	$(MAKE) guest-rootfs
	$(MAKE) deploy-ci-artifacts

CI_FIXTURES_PLAYBOOK ?= playbooks/ci-fixtures.yml

ci-fixtures-run: ## Run the selected CI fixture playbook against the existing host
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	cd ansible && ansible-playbook $(CI_FIXTURES_PLAYBOOK)

ci-fixtures-pass: CI_FIXTURES_PLAYBOOK := playbooks/ci-fixtures-pass.yml
ci-fixtures-pass: ## Run the positive CI fixture suite against the existing host
	$(MAKE) ci-fixtures-run CI_FIXTURES_PLAYBOOK=$(CI_FIXTURES_PLAYBOOK)

ci-fixtures-fail: CI_FIXTURES_PLAYBOOK := playbooks/ci-fixtures-fail.yml
ci-fixtures-fail: ## Run the negative CI fixture suite against the existing host
	$(MAKE) ci-fixtures-run CI_FIXTURES_PLAYBOOK=$(CI_FIXTURES_PLAYBOOK)

ci-fixtures-full: ci-fixtures-refresh ## Refresh artifacts, then run the pass and fail CI fixture suites together
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	cd ansible && ansible-playbook playbooks/ci-fixtures.yml --extra-vars '{"ci_fixtures_suites":["pass","fail"]}'

guest-rootfs: ## Build Alpine guest rootfs on the server
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	@test -n "$(REMOTE_HOST)" || { echo "ERROR: no ansible_host found in $(INVENTORY)"; exit 1; }
	@command -v zig >/dev/null 2>&1 || { echo "ERROR: zig is required to build homestead-smelter-guest"; exit 1; }
	@echo "→ building homestead-smelter guest (zig)"
	cd homestead-smelter && zig build -Doptimize=ReleaseSafe
	cp homestead-smelter/zig-out/bin/homestead-smelter-guest /tmp/homestead-smelter-guest 2>/dev/null || \
	cp zig-out/bin/homestead-smelter-guest /tmp/homestead-smelter-guest
	@echo "→ building forgevm-init (static, linux/amd64)"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /tmp/forgevm-init ./cmd/forgevm-init
	@echo "→ uploading build script, version pins, and forgevm-init"
	@UPLOADS="scripts/build-guest-rootfs.sh ci/versions.json /tmp/forgevm-init /tmp/homestead-smelter-guest"; \
	scp $(SSH_OPTS) $$UPLOADS $(REMOTE_USER)@$(REMOTE_HOST):/tmp/
	@echo "→ building guest rootfs on $(REMOTE_HOST)"
	ssh $(SSH_OPTS) -t $(REMOTE_USER)@$(REMOTE_HOST) \
		'cd /tmp && sudo env "PATH=$(REMOTE_PATH)" bash build-guest-rootfs.sh'

deploy-ci-artifacts: ## Deploy rootfs to /var/lib/ci/ on the server
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	@test -n "$(REMOTE_HOST)" || { echo "ERROR: no ansible_host found in $(INVENTORY)"; exit 1; }
	ssh $(SSH_OPTS) -t $(REMOTE_USER)@$(REMOTE_HOST) \
		'sudo cp /tmp/ci/output/rootfs.ext4 /var/lib/ci/rootfs.ext4 && sudo cp /tmp/ci/output/vmlinux /var/lib/ci/vmlinux && sudo cp /tmp/ci/output/sbom.txt /var/lib/ci/sbom.txt && sudo cp /tmp/ci/output/guest-artifacts.json /var/lib/ci/guest-artifacts.json'

smelter-dev: ## Hot-swap smelter guest into dev golden, boot + probe in Firecracker VM (~10s)
	@test -f $(INVENTORY) || { echo "ERROR: $(INVENTORY) not found — run 'make provision' first"; exit 1; }
	@command -v zig >/dev/null 2>&1 || { echo "ERROR: zig is required to build homestead-smelter-guest"; exit 1; }
	@echo "→ building homestead-smelter guest (zig)"
	cd homestead-smelter && zig build -Doptimize=ReleaseSafe
	@echo "→ uploading guest binary"
	scp $(SSH_OPTS) homestead-smelter/zig-out/bin/homestead-smelter-guest \
		$(REMOTE_USER)@$(REMOTE_HOST):/tmp/homestead-smelter-guest
	@echo "→ running smelter dev playbook"
	cd ansible && ansible-playbook playbooks/smelter-dev.yml

# PATH for sudo on the remote — includes Nix profile where node lives.
REMOTE_PATH := /home/$(REMOTE_USER)/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
