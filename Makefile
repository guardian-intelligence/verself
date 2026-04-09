.PHONY: build clean test test-integration lint lint-ansible fmt vet tidy \
       hooks-install doctor setup-domain inventory-check seed-system billing-reset verification-reset vm-guest-telemetry-build \
       traces clickhouse-shell clickhouse-query clickhouse-schemas mail mail-accounts mail-mailboxes mail-code mail-read edit-secrets \
       verification-repo verify-sandbox-live

VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
FM       := src/platform
VMO      := src/vm-orchestrator
BS       := src/billing-service
AM       := src/auth-middleware
SR       := src/sandbox-rental-service
MS       := src/mailbox-service
OT       := src/otel
WL       := src/workload
INVENTORY := $(FM)/ansible/inventory/hosts.ini

build: ## Build the forge-metal Go binary
	go build $(LDFLAGS) -o $(FM)/forge-metal ./$(FM)/cmd/forge-metal

vm-guest-telemetry-build: ## Build the vm-guest-telemetry Zig binary
	cd src/vm-guest-telemetry && zig build -Doptimize=ReleaseSafe

clean:
	rm -f $(FM)/forge-metal

test: ## Run unit tests
	go test -race ./$(FM)/... ./$(VMO)/... ./$(BS)/... ./$(AM)/... ./$(SR)/... ./$(MS)/... ./$(OT)/... ./$(WL)/...

test-integration: ## Run all tests including ZFS integration (requires sudo + zfs)
	@echo "Integration tests require root for ZFS pool operations."
	@echo "You may be prompted for your password."
	@echo ""
	sudo env PATH="$$PATH" go test -tags integration -race -count=1 ./$(FM)/...

lint:
	golangci-lint run ./$(FM)/... ./$(VMO)/... ./$(BS)/... ./$(AM)/... ./$(SR)/... ./$(MS)/... ./$(OT)/... ./$(WL)/...

lint-ansible:
	cd $(FM)/ansible && ansible-lint playbooks roles

hooks-install:
	@hooks_path=$$(git config --get core.hooksPath || true); \
	if [ "$$hooks_path" = "$(CURDIR)/.git/hooks" ] || [ "$$hooks_path" = ".git/hooks" ]; then \
		git config --unset-all core.hooksPath; \
	fi
	pre-commit install

fmt:
	gofumpt -w $(FM) $(VMO) $(BS) $(AM) $(SR) $(MS) $(OT) $(WL)

vet:
	go vet ./$(FM)/... ./$(VMO)/... ./$(BS)/... ./$(AM)/... ./$(SR)/... ./$(MS)/... ./$(OT)/... ./$(WL)/...

tidy:
	cd $(FM) && go mod tidy
	cd $(VMO) && go mod tidy
	cd $(BS) && go mod tidy
	cd $(AM) && go mod tidy
	cd $(SR) && go mod tidy
	cd $(MS) && go mod tidy
	cd $(OT) && go mod tidy
	cd $(WL) && go mod tidy

setup-domain: build ## Configure Cloudflare domain (interactive wizard)
	cd $(FM) && ./forge-metal setup-domain $(DOMAIN)

inventory-check: ## Validate that the generated Ansible inventory exists
	@test -f "$(INVENTORY)" || { echo "ERROR: $(INVENTORY) not found. Run: cd $(FM)/ansible && ansible-playbook playbooks/provision.yml"; exit 1; }

seed-system: inventory-check ## Seed platform + Acme tenants, billing, mailboxes, and auth verify
	cd $(FM)/ansible && ansible-playbook playbooks/seed-system.yml

billing-reset: inventory-check ## Exhaustively wipe billing state (TigerBeetle + billing PostgreSQL schema) and restart billing callers
	cd $(FM)/ansible && ansible-playbook playbooks/billing-reset.yml

verification-reset: inventory-check ## Exhaustively wipe verification state (billing, sandbox_rental, ClickHouse forge_metal + telemetry)
	cd $(FM)/ansible && ansible-playbook playbooks/verification-reset.yml

verification-repo: inventory-check ## Ensure the public local Forgejo verification repo exists and is force-pushed from the fixture
	cd $(FM) && ./scripts/ensure-verification-repo.sh

verify-sandbox-live: inventory-check ## Reset, deploy, seed, run Playwright sandbox verification, and collect evidence
	cd $(FM) && ./scripts/verify-sandbox-live.sh

traces: inventory-check ## Pull recent traces+logs: make traces [SERVICE=billing-service] [MINUTES=5] [ERRORS=1]
	cd $(FM) && ./scripts/traces.sh $(if $(SERVICE),-s $(SERVICE),) $(if $(MINUTES),-m $(MINUTES),) $(if $(ERRORS),-e,)

clickhouse-shell: inventory-check ## Open an interactive clickhouse-client session on the worker
	cd $(FM) && ./scripts/clickhouse.sh

clickhouse-query: inventory-check ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=forge_metal]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(FM) && ./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

clickhouse-schemas: inventory-check ## Print CREATE TABLE statements for all project tables
	cd $(FM) && ./scripts/clickhouse.sh --query "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('forge_metal', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw"

MAILBOX_ARG = $(if $(MAILBOX),$(MAILBOX),$(if $(filter command line,$(origin USER)),$(USER),))

mail: inventory-check ## List recent emails (defaults to agents): make mail [MAILBOX=ceo] [N=10]
	cd $(FM) && ./scripts/mail.sh list $(if $(MAILBOX_ARG),--account $(MAILBOX_ARG),) $(if $(N),--limit $(N),)

mail-accounts: inventory-check ## List synced mailbox accounts
	cd $(FM) && ./scripts/mail.sh accounts

mail-mailboxes: inventory-check ## List mailboxes for an account (defaults to agents): make mail-mailboxes [MAILBOX=ceo]
	cd $(FM) && ./scripts/mail.sh mailboxes $(if $(MAILBOX_ARG),--account $(MAILBOX_ARG),)

mail-code: inventory-check ## Extract latest 2FA/verification code (defaults to agents): make mail-code [MAILBOX=ceo]
	cd $(FM) && ./scripts/mail.sh code $(if $(MAILBOX_ARG),--account $(MAILBOX_ARG),)

mail-read: inventory-check ## Read a specific email (defaults to agents): make mail-read [MAILBOX=ceo] ID=eaaaaab
	@test -n "$(ID)" || { echo "ERROR: ID is required (get IDs from 'make mail')"; exit 1; }
	cd $(FM) && ./scripts/mail.sh read $(if $(MAILBOX_ARG),--account $(MAILBOX_ARG),) --id $(ID)

mail-send: inventory-check ## Send via Resend: make mail-send TO=agents SUBJECT='test' BODY='hello'
	@test -n "$(TO)" || { echo "ERROR: TO is required (e.g. TO=agents or TO=ceo or TO=user@example.com)"; exit 1; }
	@test -n "$(SUBJECT)" || { echo "ERROR: SUBJECT is required"; exit 1; }
	@test -n "$(BODY)" || { echo "ERROR: BODY is required"; exit 1; }
	cd $(FM) && ./scripts/mail-send.sh -t "$(TO)" -s "$(SUBJECT)" -b "$(BODY)"

mail-send-agents: inventory-check ## Send via Resend to agents inbox: make mail-send-agents SUBJECT='test' BODY='hello'
	@test -n "$(SUBJECT)" || { echo "ERROR: SUBJECT is required"; exit 1; }
	@test -n "$(BODY)" || { echo "ERROR: BODY is required"; exit 1; }
	cd $(FM) && ./scripts/mail-send.sh -t agents -s "$(SUBJECT)" -b "$(BODY)"

mail-send-ceo: inventory-check ## Send via Resend to ceo inbox: make mail-send-ceo SUBJECT='test' BODY='hello'
	@test -n "$(SUBJECT)" || { echo "ERROR: SUBJECT is required"; exit 1; }
	@test -n "$(BODY)" || { echo "ERROR: BODY is required"; exit 1; }
	cd $(FM) && ./scripts/mail-send.sh -t ceo -s "$(SUBJECT)" -b "$(BODY)"

mail-passwords: inventory-check ## Show Stalwart mailbox passwords
	@echo "ceo@$$(cd $(FM) && grep '^forge_metal_domain:' ansible/group_vars/all/main.yml | awk '{print $$2}' | tr -d '\"'):"
	@cd $(FM) && sops -d --extract '["stalwart_ceo_password"]' ansible/group_vars/all/secrets.sops.yml
	@echo ""
	@echo "agents@$$(cd $(FM) && grep '^forge_metal_domain:' ansible/group_vars/all/main.yml | awk '{print $$2}' | tr -d '\"'):"
	@cd $(FM) && sops -d --extract '["stalwart_agents_password"]' ansible/group_vars/all/secrets.sops.yml

edit-secrets: ## Open encrypted secrets in $$EDITOR via sops
	sops $(FM)/ansible/group_vars/all/secrets.sops.yml
