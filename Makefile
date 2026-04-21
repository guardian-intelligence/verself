.PHONY: help test lint lint-scripts lint-conversions lint-ansible lint-voice company-proof fmt vet tidy openapi openapi-check openapi-wire-check \
       hooks-install doctor inventory-check setup-dev setup-sops provision deprovision deploy site guest-rootfs security-patch identity-reset seed-system assume-persona assume-platform-admin assume-acme-admin assume-acme-member \
       set-user-state billing-clock billing-wall-clock billing-state billing-documents billing-finalizations billing-events billing-pg-shell billing-pg-query billing-proof billing-reset verification-reset \
       secrets-proof secrets-leak-proof openbao-proof openbao-tenancy-proof workload-identity-proof \
       vm-guest-telemetry-build observe telemetry-proof telemetry-proof-fail clickhouse-query clickhouse-schemas pg-shell pg-query pg-list tb-shell tb-command mail mail-accounts mail-mailboxes \
       mail-code mail-read mail-send mail-send-agents mail-send-ceo mail-passwords edit-secrets \
       wipe-pg-db wipe-server vm-orchestrator-proof stress sandbox-inner sandbox-middle sandbox-proof rent-ui-smoke rent-ui-local rent-local-dev scheduler-proof verify-scheduler grafana-proof observability-smoke services-doctor

FM       := src/platform
AW       := src/apiwire
VMO      := src/vm-orchestrator
BS       := src/billing-service
GS       := src/governance-service
IS       := src/identity-service
SS       := src/secrets-service
AM       := src/auth-middleware
SR       := src/sandbox-rental-service
MS       := src/mailbox-service
OT       := src/otel
INVENTORY := $(FM)/ansible/inventory/hosts.ini
GO_DIRS  := $(AW) $(VMO) $(BS) $(GS) $(IS) $(SS) $(AM) $(SR) $(MS) $(OT)
GO_PKGS  := $(addsuffix /...,$(addprefix ./,$(GO_DIRS)))
BILLING_PRODUCT_ID ?= sandbox
ASSUME_PERSONA_OUTPUT_FLAG := $(if $(OUTPUT),--output "$(OUTPUT)",)
ASSUME_PERSONA_PRINT_FLAG := $(if $(filter 1 true yes,$(PRINT)),--print,)
ASSUME_PERSONA_FLAGS := $(ASSUME_PERSONA_OUTPUT_FLAG) $(ASSUME_PERSONA_PRINT_FLAG)

help: ## Show available root automation targets
	@awk 'BEGIN {FS = ":.*## "; printf "Forge Metal targets:\n"} /^[A-Za-z0-9_.-]+:.*## / {printf "  %-32s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

vm-guest-telemetry-build: ## Build the vm-guest-telemetry Zig binary
	cd src/vm-guest-telemetry && zig build -Doptimize=ReleaseSafe

test: ## Run unit tests
	go test -race $(GO_PKGS)

lint: lint-conversions
	golangci-lint run $(GO_PKGS)

lint-scripts: ## Run ShellCheck over platform shell scripts
	shellcheck -x -P . $(FM)/scripts/*.sh $(FM)/scripts/lib/*.sh $(FM)/scripts/security/*.sh

lint-conversions:
	gosec -quiet -include=G115 $(GO_PKGS)

lint-ansible:
	cd $(FM)/ansible && ansible-lint playbooks roles

lint-voice: ## Scan apps/company content for banned words and BuzzFeed hooks (Guardian voice spec)
	cd src/viteplus-monorepo && corepack pnpm --filter "@forge-metal/company" run lint:voice

company-proof: inventory-check ## Walk the Guardian Intelligence site IA, exercise OG + brand kit, verify company.* spans in ClickHouse
	cd $(FM) && ./scripts/verify-company-live.sh

hooks-install:
	@hooks_path=$$(git config --get core.hooksPath || true); \
	if [ "$$hooks_path" = "$(CURDIR)/.git/hooks" ] || [ "$$hooks_path" = ".git/hooks" ]; then \
		git config --unset-all core.hooksPath; \
	fi
	pre-commit install

fmt:
	gofumpt -w $(GO_DIRS)

vet:
	go vet $(GO_PKGS)

tidy:
	cd $(AW) && go mod tidy
	cd $(VMO) && go mod tidy
	cd $(BS) && go mod tidy
	cd $(GS) && go mod tidy
	cd $(IS) && go mod tidy
	cd $(SS) && go mod tidy
	cd $(AM) && go mod tidy
	cd $(SR) && go mod tidy
	cd $(MS) && go mod tidy
	cd $(OT) && go mod tidy
	cd src/viteplus-monorepo && vp fmt . --write

openapi: ## Regenerate committed OpenAPI 3.0 and 3.1 specs for Go services
	go run ./$(BS)/cmd/billing-openapi --format 3.0 > $(BS)/openapi/openapi-3.0.yaml
	go run ./$(BS)/cmd/billing-openapi --format 3.1 > $(BS)/openapi/openapi-3.1.yaml
	mkdir -p $(GS)/openapi
	go run ./$(GS)/cmd/governance-openapi --format 3.0 > $(GS)/openapi/openapi-3.0.yaml
	go run ./$(GS)/cmd/governance-openapi --format 3.1 > $(GS)/openapi/openapi-3.1.yaml
	mkdir -p $(IS)/openapi
	go run ./$(IS)/cmd/identity-openapi --format 3.0 > $(IS)/openapi/openapi-3.0.yaml
	go run ./$(IS)/cmd/identity-openapi --format 3.1 > $(IS)/openapi/openapi-3.1.yaml
	mkdir -p $(SS)/openapi
	go run ./$(SS)/cmd/secrets-openapi --format 3.0 > $(SS)/openapi/openapi-3.0.yaml
	go run ./$(SS)/cmd/secrets-openapi --format 3.1 > $(SS)/openapi/openapi-3.1.yaml
	go run ./$(MS)/cmd/mailbox-openapi --format 3.0 > $(MS)/openapi/openapi-3.0.yaml
	go run ./$(MS)/cmd/mailbox-openapi --format 3.1 > $(MS)/openapi/openapi-3.1.yaml
	mkdir -p $(SR)/openapi
	go run ./$(SR)/cmd/sandbox-rental-openapi --format 3.0 > $(SR)/openapi/openapi-3.0.yaml
	go run ./$(SR)/cmd/sandbox-rental-openapi --format 3.1 > $(SR)/openapi/openapi-3.1.yaml

openapi-check: ## Verify committed OpenAPI specs are up to date
	cd $(BS) && go run ./cmd/billing-openapi --format 3.0 --check
	cd $(BS) && go run ./cmd/billing-openapi --format 3.1 --check
	cd $(GS) && go run ./cmd/governance-openapi --format 3.0 --check
	cd $(GS) && go run ./cmd/governance-openapi --format 3.1 --check
	cd $(IS) && go run ./cmd/identity-openapi --format 3.0 --check
	cd $(IS) && go run ./cmd/identity-openapi --format 3.1 --check
	cd $(SS) && go run ./cmd/secrets-openapi --format 3.0 --check
	cd $(SS) && go run ./cmd/secrets-openapi --format 3.1 --check
	cd $(MS) && go run ./cmd/mailbox-openapi --format 3.0 --check
	cd $(MS) && go run ./cmd/mailbox-openapi --format 3.1 --check
	cd $(SR) && go run ./cmd/sandbox-rental-openapi --format 3.0 --check
	cd $(SR) && go run ./cmd/sandbox-rental-openapi --format 3.1 --check
	$(MAKE) openapi-wire-check

openapi-wire-check: ## Verify frontend-consumed OpenAPI 3.1 specs are JS wire-safe
	go run ./$(AW)/cmd/openapi-wire-check \
		$(BS)/openapi/openapi-3.1.yaml \
		$(GS)/openapi/openapi-3.1.yaml \
		$(IS)/openapi/openapi-3.1.yaml \
		$(SS)/openapi/openapi-3.1.yaml \
		$(MS)/openapi/openapi-3.1.yaml \
		$(SR)/openapi/openapi-3.1.yaml

inventory-check: ## Validate that the generated Ansible inventory exists
	@test -f "$(INVENTORY)" || { echo "ERROR: $(INVENTORY) not found. Run: cd $(FM)/ansible && ansible-playbook playbooks/provision.yml"; exit 1; }

setup-dev: ## Install pinned controller dev tools
	cd $(FM)/ansible && ansible-playbook playbooks/setup-dev.yml

setup-sops: ## Bootstrap SOPS + Age encryption
	cd $(FM)/ansible && ansible-playbook playbooks/setup-sops.yml

provision: ## Provision bare metal and generate inventory
	cd $(FM)/ansible && ansible-playbook playbooks/provision.yml

deprovision: ## Destroy provisioned bare metal infrastructure: make deprovision CONFIRM=deprovision
	@test "$(CONFIRM)" = "deprovision" || { echo "ERROR: deprovision requires CONFIRM=deprovision"; exit 1; }
	cd $(FM)/ansible && ansible-playbook playbooks/deprovision.yml

deploy: inventory-check ## Deploy single-node environment: make deploy [TAGS=billing_service,caddy]
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/dev-single-node.yml $(if $(TAGS),--tags "$(TAGS)",)

site: inventory-check ## Deploy multi-node site playbook
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/site.yml $(if $(TAGS),--tags "$(TAGS)",)

guest-rootfs: inventory-check ## Build and stage Firecracker guest artifacts
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/guest-rootfs.yml $(if $(TAGS),--tags "$(TAGS)",)

security-patch: inventory-check ## Apply OS security updates through Ansible
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/security-patch.yml

identity-reset: inventory-check ## Exhaustively wipe identity-service PostgreSQL state and restart dependents
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/identity-reset.yml

seed-system: inventory-check ## Seed platform + Acme tenants, billing, mailboxes, and auth verify
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/seed-system.yml

assume-persona: inventory-check ## Useful utility: write persona env file: make assume-persona PERSONA=platform-admin [OUTPUT=path] [PRINT=1]
	@test -n "$(PERSONA)" || { echo "ERROR: PERSONA is required (platform-admin, acme-admin, acme-member)"; exit 1; }
	@cd $(FM) && ./scripts/assume-persona.sh "$(PERSONA)" $(ASSUME_PERSONA_FLAGS)

assume-platform-admin: inventory-check ## Useful utility: write env for platform admin agent persona
	@cd $(FM) && ./scripts/assume-persona.sh platform-admin $(ASSUME_PERSONA_FLAGS)

assume-acme-admin: inventory-check ## Useful utility: write env for Acme org admin persona
	@cd $(FM) && ./scripts/assume-persona.sh acme-admin $(ASSUME_PERSONA_FLAGS)

assume-acme-member: inventory-check ## Useful utility: write env for Acme org member persona
	@cd $(FM) && ./scripts/assume-persona.sh acme-member $(ASSUME_PERSONA_FLAGS)

set-user-state: inventory-check ## Set billing fixture state: make set-user-state EMAIL=ceo@example.com ORG=platform STATE=pro [BALANCE_CENTS=10000]
	@cd $(FM) && ./scripts/set-user-state.sh \
		--email "$(EMAIL)" \
		--org "$(ORG)" \
		--org-id "$(ORG_ID)" \
		--org-name "$(ORG_NAME)" \
		--state "$(STATE)" \
		--plan-id "$(PLAN_ID)" \
		--product-id "$(BILLING_PRODUCT_ID)" \
		--balance-units "$(BALANCE_UNITS)" \
		--balance-cents "$(BALANCE_CENTS)" \
		--business-now "$(BUSINESS_NOW)" \
		--overage-policy "$(OVERAGE_POLICY)" \
		--trust-tier "$(TRUST_TIER)"

billing-clock: inventory-check ## Inspect or mutate billing business time: make billing-clock ORG_ID=123 [SET=...|ADVANCE_SECONDS=...|CLEAR=1]
	@cd $(FM) && ./scripts/billing-clock.sh \
		--org "$(ORG)" \
		--org-id "$(ORG_ID)" \
		--product-id "$(BILLING_PRODUCT_ID)" \
		$(if $(SET),--set "$(SET)",) \
		$(if $(ADVANCE_SECONDS),--advance-seconds "$(ADVANCE_SECONDS)",) \
		$(if $(CLEAR),--clear,) \
		$(if $(REASON),--reason "$(REASON)",)

billing-wall-clock: inventory-check ## Reset billing test time to wall-clock and repair current cycle: make billing-wall-clock ORG=platform
	@cd $(FM) && ./scripts/billing-clock.sh \
		--org "$(ORG)" \
		--org-id "$(ORG_ID)" \
		--product-id "$(BILLING_PRODUCT_ID)" \
		--wall-clock \
		$(if $(REASON),--reason "$(REASON)",)

billing-state: inventory-check ## Inspect billing state for an org: make billing-state ORG=platform [BILLING_PRODUCT_ID=sandbox]
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(FM) && ./scripts/billing-inspect.sh --kind state --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-documents: inventory-check ## List billing documents for an org: make billing-documents ORG=platform
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(FM) && ./scripts/billing-inspect.sh --kind documents --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-finalizations: inventory-check ## List billing finalizations for an org: make billing-finalizations ORG=platform
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(FM) && ./scripts/billing-inspect.sh --kind finalizations --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-events: inventory-check ## Query recent billing events in ClickHouse: make billing-events [ORG_ID=123] [EVENT=billing_document_issued] [MINUTES=60]
	cd $(FM) && ./scripts/billing-inspect.sh --kind events --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(EVENT),--event-type "$(EVENT)",) $(if $(MINUTES),--minutes "$(MINUTES)",) $(if $(FORMAT),--format "$(FORMAT)",)

billing-pg-shell: inventory-check ## Open psql against the billing database
	cd $(FM) && ./scripts/pg.sh billing

billing-pg-query: inventory-check ## Run a PostgreSQL query against billing: make billing-pg-query QUERY='SELECT 1'
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(FM) && ./scripts/pg.sh billing --query "$(QUERY)"

billing-proof: inventory-check ## Run live billing browser proof and collect evidence
	cd $(FM) && ./scripts/verify-rent-billing-flow.sh

secrets-proof: inventory-check ## Run live secrets API and sandbox injection proof
	cd $(FM) && ./scripts/verify-secrets-live.sh

secrets-leak-proof: inventory-check ## Prove bearer/JWT material is absent from traces, logs, audit rows, and proof artifacts
	cd $(FM) && ./scripts/verify-secrets-leak-proof.sh

openbao-proof: inventory-check ## Prove OpenBao process, health, metrics, audit log, nftables, and ClickHouse evidence
	cd $(FM) && ./scripts/verify-openbao-live.sh

openbao-tenancy-proof: inventory-check ## Prove OpenBao per-org mounts, JWT roles, SPIFFE workload roles, policies, and ClickHouse spans
	cd $(FM) && ./scripts/verify-openbao-tenancy-live.sh

workload-identity-proof: inventory-check ## Prove SPIFFE mTLS/JWT-SVID boundaries, SPIRE bundle JWKS, stale credential deletion, and ClickHouse evidence
	cd $(FM) && ./scripts/verify-workload-identity-live.sh

billing-reset: inventory-check ## Exhaustively wipe billing state (TigerBeetle + billing PostgreSQL schema) and restart billing callers
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/billing-reset.yml

verification-reset: inventory-check ## Exhaustively wipe verification state (billing, sandbox_rental, ClickHouse forge_metal + telemetry)
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/verification-reset.yml

wipe-pg-db: inventory-check ## Wipe one managed PostgreSQL service DB: make wipe-pg-db DB=sandbox_rental
	@test -n "$(DB)" || { echo "ERROR: DB is required (billing|sandbox_rental|mailbox_service|identity_service)"; exit 1; }
	$(FM)/scripts/ansible-with-tunnel.sh playbooks/wipe-pg-db.yml -e "wipe_pg_db_name=$(DB)"

vm-orchestrator-proof: inventory-check ## Live proof for vm-orchestrator lease/exec spans through the public sandbox API
	cd $(FM) && ./scripts/verify-vm-orchestrator-live.sh

stress: inventory-check ## Burst N parallel sandbox submissions to produce a real p50/p99/p100 distribution. Bypasses the full reseed. Usage: make stress [SUBMISSIONS=200] [PARALLEL=8] [PROFILE=echo|cpu-mem|disk|mixed] [TIMEOUT=1800]
	cd $(FM) && \
	  SANDBOX_PROOF_SUBMISSIONS="$(if $(SUBMISSIONS),$(SUBMISSIONS),200)" \
	  SANDBOX_PROOF_SUBMIT_PARALLEL="$(if $(PARALLEL),$(PARALLEL),8)" \
	  SANDBOX_PROOF_WORKLOAD_PROFILE="$(if $(PROFILE),$(PROFILE),echo)" \
	  SANDBOX_PROOF_TIMEOUT_SECONDS="$(if $(TIMEOUT),$(TIMEOUT),1800)" \
	  ./scripts/verify-sandbox-public-api.sh

sandbox-inner: inventory-check ## Inner loop: default starts local HMR; use SANDBOX_INNER_MODE=verify for local smoke evidence
	cd $(FM) && ./scripts/sandbox-inner.sh

sandbox-middle: inventory-check ## Middle loop: default deploys UI and runs admin smoke; use SANDBOX_DEPLOY_TARGET=ui|service|both|none SANDBOX_VERIFY_TARGET=admin|execute|billing|none SANDBOX_SEED_VERIFY=1
	cd $(FM) && ./scripts/sandbox-middle.sh

sandbox-proof: inventory-check ## Proof loop: full reset, redeploy, reseed, and live full-lifecycle sandbox verification
	cd $(FM) && ./scripts/verify-sandbox-live.sh

rent-ui-smoke: inventory-check ## Run deployed rent-a-sandbox authenticated shell smoke
	cd $(FM) && TEST_BASE_URL="$${TEST_BASE_URL:-https://rentasandbox.$$(awk -F'\"' '/^forge_metal_domain:/{print $$2}' ansible/group_vars/all/main.yml)}" ./scripts/verify-rent-ui-smoke.sh

rent-ui-local: inventory-check ## Run rent-a-sandbox smoke against local HMR dev server
	cd $(FM) && ./scripts/verify-rent-ui-local.sh

rent-local-dev: inventory-check ## Start local rent-a-sandbox dev tunnels and HMR server
	cd $(FM) && ./scripts/run-rent-local-dev.sh $(if $(PRINT_ENV),--print-env,)

rentasandbox-frontend-deploy-fast: inventory-check ## Ship UI-only changes to rent-a-sandbox: local build + rsync .output/ + restart (~5-10s). For API/env/systemd/OIDC changes use `ansible-playbook ... --tags rent_a_sandbox`.
	$(FM)/scripts/rentasandbox-frontend-deploy-fast.sh

platform-frontend-deploy-fast: inventory-check ## Ship UI-only changes to platform docs: local build + rsync .output/ + restart (~5-10s). For env/systemd/nftables/Caddy changes use `ansible-playbook ... --tags platform`.
	$(FM)/scripts/platform-frontend-deploy-fast.sh

platform-local-dev: ## Start local platform docs HMR dev server (no tunnels; no service deps)
	cd src/viteplus-monorepo/apps/platform && FORGE_METAL_DOMAIN=$$(awk -F'"' '/^forge_metal_domain:/{print $$2}' $(FM)/ansible/group_vars/all/main.yml) vp dev

scheduler-proof: inventory-check ## Proof loop: enqueue a River scheduler probe and assert PG + ClickHouse evidence
	cd $(FM) && ./scripts/verify-scheduler-runtime.sh

verify-scheduler: scheduler-proof

grafana-proof: inventory-check ## Verify Grafana health, datasource execution, PostgreSQL state, and ClickHouse evidence
	cd $(FM) && ./scripts/verify-grafana-live.sh

services-doctor: inventory-check ## Cross-check declared services.yml against live listeners on the box: make services-doctor [FORMAT=table|json|nftables]
	@python3 $(FM)/scripts/services-doctor.py

observe: inventory-check ## Discover/query telemetry: make observe [WHAT=catalog|queries|describe|metric|trace|logs|http|service|errors|mail|deploy|workload-identity] [SIGNAL=...] [FORMAT=table|json|markdown]
	cd $(FM) && ./scripts/observe.sh $(if $(WHAT),--what "$(WHAT)",) $(if $(SIGNAL),--signal "$(SIGNAL)",) $(if $(SERVICE),--service "$(SERVICE)",) $(if $(METRIC),--metric "$(METRIC)",) $(if $(SPAN),--span "$(SPAN)",) $(if $(FIELD),--field "$(FIELD)",) $(if $(QUERY),--query "$(QUERY)",) $(if $(PREFIX),--prefix "$(PREFIX)",) $(if $(SEARCH),--search "$(SEARCH)",) $(if $(GROUP_BY),--group-by "$(GROUP_BY)",) $(if $(MODE),--mode "$(MODE)",) $(if $(TRACE_ID),--trace-id "$(TRACE_ID)",) $(if $(RUN_KEY),--run-key "$(RUN_KEY)",) $(if $(HOST),--host "$(HOST)",) $(if $(STATUS_MIN),--status-min "$(STATUS_MIN)",) $(if $(FORMAT),--format "$(FORMAT)",) $(if $(MINUTES),--minutes "$(MINUTES)",) $(if $(LIMIT),--limit "$(LIMIT)",) $(if $(ERRORS),--errors,)

telemetry-proof: inventory-check ## Run observability smoke and verify ansible spans land in ClickHouse
	cd $(FM) && ./scripts/telemetry-proof.sh

telemetry-proof-fail: inventory-check ## Run observability smoke fail-path and verify Error spans land in ClickHouse
	cd $(FM) && TELEMETRY_PROOF_EXPECT_FAIL=1 ./scripts/telemetry-proof.sh

observability-smoke: inventory-check ## Run the raw Ansible observability smoke playbook
	cd $(FM)/ansible && ansible-playbook playbooks/observability-smoke.yml

clickhouse-query: inventory-check ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=forge_metal]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(FM) && ./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

clickhouse-schemas: inventory-check ## Print CREATE TABLE statements for all project tables
	cd $(FM) && ./scripts/clickhouse.sh --query "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('forge_metal', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw"

pg-list: inventory-check ## List PostgreSQL databases on the worker (authoritative via \l)
	cd $(FM) && ./scripts/pg.sh --list

pg-shell: inventory-check ## Open interactive psql: make pg-shell DB=sandbox_rental (run 'make pg-list' to see databases)
	@test -n "$(DB)" || { echo "ERROR: DB is required (run 'make pg-list' to see databases)"; exit 1; }
	cd $(FM) && ./scripts/pg.sh "$(DB)"

pg-query: inventory-check ## Run a PostgreSQL query on the worker: make pg-query DB=sandbox_rental QUERY='SELECT 1'
	@test -n "$(DB)" || { echo "ERROR: DB is required (run 'make pg-list' to see databases)"; exit 1; }
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(FM) && ./scripts/pg.sh "$(DB)" --query "$(QUERY)"

tb-shell: inventory-check ## Open the TigerBeetle REPL (Ctrl+D to exit). Ops: create_accounts, create_transfers, lookup_accounts, lookup_transfers, get_account_transfers, get_account_balances, query_accounts, query_transfers
	cd $(FM) && ./scripts/tigerbeetle.sh

tb-command: inventory-check ## Run a single TigerBeetle REPL op: make tb-command COMMAND='query_accounts limit=10;'
	@test -n "$(COMMAND)" || { echo "ERROR: COMMAND is required (e.g. 'lookup_accounts id=1;')"; exit 1; }
	cd $(FM) && ./scripts/tigerbeetle.sh --command "$(COMMAND)"

MAILBOX_ARG = $(if $(MAILBOX),$(MAILBOX),$(if $(filter command line,$(origin USER)),$(USER),))
MAILBOX_ACCOUNT_FLAG = $(if $(MAILBOX_ARG),--account $(MAILBOX_ARG),)
MAILBOX_TOOL = cd $(MS) && go run ./cmd/mailbox-tool --inventory "$(abspath $(INVENTORY))"

mail: inventory-check ## List recent emails (defaults to agents): make mail [MAILBOX=ceo] [N=10]
	$(MAILBOX_TOOL) list $(MAILBOX_ACCOUNT_FLAG) $(if $(N),--limit $(N),)

mail-accounts: inventory-check ## List synced mailbox accounts
	$(MAILBOX_TOOL) accounts

mail-mailboxes: inventory-check ## List mailboxes for an account (defaults to agents): make mail-mailboxes [MAILBOX=ceo]
	$(MAILBOX_TOOL) mailboxes $(MAILBOX_ACCOUNT_FLAG)

mail-code: inventory-check ## Extract latest 2FA/verification code (defaults to agents): make mail-code [MAILBOX=ceo]
	$(MAILBOX_TOOL) code $(MAILBOX_ACCOUNT_FLAG)

mail-read: inventory-check ## Read a specific email (defaults to agents): make mail-read [MAILBOX=ceo] ID=eaaaaab
	@test -n "$(ID)" || { echo "ERROR: ID is required (get IDs from 'make mail')"; exit 1; }
	$(MAILBOX_TOOL) read $(MAILBOX_ACCOUNT_FLAG) --id $(ID)

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

wipe-server: inventory-check ## Wipe all forge-metal state from the provisioned server: make wipe-server CONFIRM=wipe-server
	@test "$(CONFIRM)" = "wipe-server" || { echo "ERROR: wipe-server requires CONFIRM=wipe-server"; exit 1; }
	cd $(FM) && ./scripts/wipe-server.sh
