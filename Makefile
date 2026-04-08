.PHONY: build clean test test-integration lint lint-ansible fmt vet tidy \
       hooks-install doctor setup-domain inventory-check seed-demo billing-reset vm-guest-telemetry-build \
       traces clickhouse-shell clickhouse-query clickhouse-schemas mail mail-code mail-read edit-secrets

VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
FM       := src/platform
VMO      := src/vm-orchestrator
BS       := src/billing-service
BL       := src/billing
AM       := src/auth-middleware
BC       := src/billing-client
SR       := src/sandbox-rental-service
INVENTORY := $(FM)/ansible/inventory/hosts.ini

build: ## Build the forge-metal Go binary
	go build $(LDFLAGS) -o $(FM)/forge-metal ./$(FM)/cmd/forge-metal

vm-guest-telemetry-build: ## Build the vm-guest-telemetry Zig binary
	cd src/vm-guest-telemetry && zig build -Doptimize=ReleaseSafe

clean:
	rm -f $(FM)/forge-metal

test: ## Run unit tests
	go test -race ./$(FM)/... ./$(VMO)/...
	go test -race -parallel=1 ./$(BL)/...
	go test -race ./$(BS)/... ./$(AM)/... ./$(BC)/... ./$(SR)/...

test-integration: ## Run all tests including ZFS integration (requires sudo + zfs)
	@echo "Integration tests require root for ZFS pool operations."
	@echo "You may be prompted for your password."
	@echo ""
	sudo env PATH="$$PATH" go test -tags integration -race -count=1 ./$(FM)/...

lint:
	golangci-lint run ./$(FM)/... ./$(VMO)/... ./$(BL)/... ./$(BS)/... ./$(AM)/... ./$(BC)/... ./$(SR)/...

lint-ansible:
	cd $(FM)/ansible && ansible-lint playbooks roles

hooks-install:
	@hooks_path=$$(git config --get core.hooksPath || true); \
	if [ "$$hooks_path" = "$(CURDIR)/.git/hooks" ] || [ "$$hooks_path" = ".git/hooks" ]; then \
		git config --unset-all core.hooksPath; \
	fi
	pre-commit install

fmt:
	gofumpt -w $(FM) $(VMO) $(BL) $(BS) $(AM) $(BC) $(SR)

vet:
	go vet ./$(FM)/... ./$(VMO)/... ./$(BL)/... ./$(BS)/... ./$(AM)/... ./$(BC)/... ./$(SR)/...

tidy:
	cd $(FM) && go mod tidy
	cd $(VMO) && go mod tidy
	cd $(BL) && go mod tidy
	cd $(BS) && go mod tidy
	cd $(AM) && go mod tidy
	cd $(BC) && go mod tidy
	cd $(SR) && go mod tidy

setup-domain: build ## Configure Cloudflare domain (interactive wizard)
	cd $(FM) && ./forge-metal setup-domain $(DOMAIN)

inventory-check: ## Validate that the generated Ansible inventory exists
	@test -f "$(INVENTORY)" || { echo "ERROR: $(INVENTORY) not found. Run: cd $(FM)/ansible && ansible-playbook playbooks/provision.yml"; exit 1; }

seed-demo: inventory-check ## Seed demo environment: human user + billing catalog + credits + auth verify
	cd $(FM)/ansible && ansible-playbook playbooks/seed-demo.yml

billing-reset: inventory-check ## Exhaustively wipe billing state (TigerBeetle + billing PostgreSQL schema) and restart billing callers
	cd $(FM)/ansible && ansible-playbook playbooks/billing-reset.yml

traces: inventory-check ## Pull recent traces+logs: make traces [SERVICE=billing-service] [MINUTES=5] [ERRORS=1]
	cd $(FM) && ./scripts/traces.sh $(if $(SERVICE),-s $(SERVICE),) $(if $(MINUTES),-m $(MINUTES),) $(if $(ERRORS),-e,)

clickhouse-shell: inventory-check ## Open an interactive clickhouse-client session on the worker
	cd $(FM) && ./scripts/clickhouse.sh

clickhouse-query: inventory-check ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=forge_metal]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(FM) && ./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

clickhouse-schemas: inventory-check ## Print CREATE TABLE statements for all project tables
	cd $(FM) && ./scripts/clickhouse.sh --query "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('forge_metal', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw"

mail: inventory-check ## List inbox: make mail [USER=bernoulli.agent] [N=10]
	cd $(FM) && ./scripts/mail.sh $(if $(USER),-u $(USER),) $(if $(N),-n $(N),)

mail-code: inventory-check ## Extract latest 2FA/verification code: make mail-code USER=bernoulli.agent
	@test -n "$(USER)" || { echo "ERROR: USER is required (e.g. USER=bernoulli.agent)"; exit 1; }
	cd $(FM) && ./scripts/mail.sh -u $(USER) -c

mail-read: inventory-check ## Read a specific email: make mail-read USER=bernoulli.agent ID=eaaaaab
	@test -n "$(USER)" || { echo "ERROR: USER is required"; exit 1; }
	@test -n "$(ID)" || { echo "ERROR: ID is required (get IDs from 'make mail')"; exit 1; }
	cd $(FM) && ./scripts/mail.sh -u $(USER) -r $(ID)

edit-secrets: ## Open encrypted secrets in $$EDITOR via sops
	sops $(FM)/ansible/group_vars/all/secrets.sops.yml
