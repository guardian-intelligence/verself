.PHONY: help bazel-doctor bazel-proof bazel-gazelle bazel-tidy bazel-update test lint lint-scripts lint-conversions lint-ansible lint-voice company-proof fmt vet tidy sqlc sqlc-check openapi openapi-check openapi-clients openapi-clients-check openapi-wire-check topology-generate topology-check topology-proof \
       hooks-install doctor inventory-check setup-dev setup-sops provision deprovision deploy guest-rootfs security-patch identity-reset seed-system assume-persona assume-platform-admin assume-acme-admin assume-acme-member \
       set-user-state billing-clock billing-wall-clock billing-state billing-documents billing-finalizations billing-events billing-pg-shell billing-pg-query billing-proof billing-reset verification-reset \
       profile-proof organization-sync-proof notifications-proof projects-proof source-code-hosting-proof secrets-proof secrets-leak-proof openbao-proof openbao-tenancy-proof workload-identity-proof spiffe-rotation-proof object-storage-verify temporal-verify temporal-web-proof recurring-schedule-proof \
       vm-guest-telemetry-build guest-artifacts-bundle observe telemetry-proof telemetry-proof-fail clickhouse-query clickhouse-schemas pg-shell pg-query pg-list tb-shell tb-command mail mail-accounts mail-mailboxes \
       mail-code mail-read mail-send mail-send-agents mail-send-ceo mail-passwords edit-secrets \
       wipe-pg-db wipe-server vm-orchestrator-proof sandbox-inner sandbox-middle sandbox-proof console-ui-smoke console-ui-local console-local-dev console-frontend-deploy-fast grafana-proof observability-smoke services-doctor

PLATFORM_DIR := src/platform
AW       := src/apiwire
VMO      := src/vm-orchestrator
BS       := src/billing-service
GS       := src/governance-service
IS       := src/identity-service
SS       := src/secrets-service
SCH      := src/source-code-hosting-service
AM       := src/auth-middleware
SR       := src/sandbox-rental-service
MS       := src/mailbox-service
OSS      := src/object-storage-service
PS       := src/profile-service
NS       := src/notifications-service
PJS      := src/projects-service
OT       := src/otel
TP       := src/temporal-platform
EC       := src/envconfig
HS       := src/httpserver
INVENTORY := $(PLATFORM_DIR)/ansible/inventory/hosts.ini
GO_DIRS  := $(AW) $(VMO) $(BS) $(GS) $(IS) $(SS) $(SCH) $(AM) $(SR) $(MS) $(OSS) $(PS) $(NS) $(PJS) $(OT) $(TP) $(EC) $(HS)
GO_CLIENT_DIRS := $(BS)/client $(GS)/client $(GS)/internalclient $(IS)/client $(IS)/internalclient $(SS)/client $(SS)/internalclient $(SCH)/client $(SCH)/internalclient $(SR)/client $(SR)/internalclient $(MS)/client $(OSS)/client $(PS)/client $(PS)/internalclient $(NS)/client $(PJS)/client $(PJS)/internalclient
GO_CLIENT_FILES := $(addsuffix /client.gen.go,$(GO_CLIENT_DIRS))
SQLC_DIRS := $(sort $(dir $(shell find src -mindepth 2 -maxdepth 2 -name sqlc.yaml -print)))
BILLING_PRODUCT_ID ?= sandbox
BAZELISK ?= bazelisk
ASSUME_PERSONA_OUTPUT_FLAG := $(if $(OUTPUT),--output "$(OUTPUT)",)
ASSUME_PERSONA_PRINT_FLAG := $(if $(filter 1 true yes,$(PRINT)),--print,)
ASSUME_PERSONA_FLAGS := $(ASSUME_PERSONA_OUTPUT_FLAG) $(ASSUME_PERSONA_PRINT_FLAG)

help: ## Show available root automation targets
	@awk 'BEGIN {FS = ":.*## "; printf "Verself targets:\n"} /^[A-Za-z0-9_.-]+:.*## / {printf "  %-32s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

bazel-doctor: ## Verify the pinned Bazel/Bazelisk bootstrap contract
	$(BAZELISK) run //tools/bazel:doctor

bazel-proof: inventory-check ## Prove Bazel bootstrap state with a ClickHouse trace assertion
	cd $(PLATFORM_DIR) && BAZELISK="$(BAZELISK)" ./scripts/verify-bazel-live.sh

bazel-gazelle: ## Regenerate Bazel Go BUILD files
	$(BAZELISK) run //:gazelle -- update -go_naming_convention=import_alias -go_naming_convention_external=import_alias

bazel-tidy: ## Update Bzlmod repository wiring from Bazel-managed module metadata
	$(BAZELISK) mod tidy --lockfile_mode=update

bazel-update: bazel-gazelle bazel-tidy ## Regenerate Gazelle BUILD files and tidy Bzlmod repository wiring

vm-guest-telemetry-build: ## Build the vm-guest-telemetry guest binary with Bazel
	$(BAZELISK) build //src/vm-guest-telemetry:vm-guest-telemetry

guest-artifacts-bundle: ## Build the Bazel guest artifact input bundle
	$(BAZELISK) build //src/platform/guest:guest_artifacts_bundle

test: ## Run unit tests
	@set -e; for dir in $(GO_DIRS); do echo "==> $$dir"; (cd "$$dir" && go test -race ./...); done

lint: lint-conversions
	@set -e; for dir in $(GO_DIRS); do echo "==> $$dir"; (cd "$$dir" && golangci-lint run ./...); done

lint-scripts: ## Run ShellCheck over platform shell scripts
	shellcheck -x -P . $(PLATFORM_DIR)/scripts/*.sh $(PLATFORM_DIR)/scripts/lib/*.sh $(PLATFORM_DIR)/scripts/security/*.sh

lint-conversions:
	@set -e; for dir in $(GO_DIRS); do echo "==> $$dir"; (cd "$$dir" && gosec -quiet -include=G115 ./...); done

lint-ansible:
	cd $(PLATFORM_DIR)/ansible && ansible-lint playbooks roles

lint-voice: ## Scan apps/company content for banned words and BuzzFeed hooks (Guardian voice spec)
	cd src/viteplus-monorepo && corepack pnpm --filter "@verself/company" run lint:voice

company-proof: inventory-check ## Walk the Guardian Intelligence site IA, exercise OG + brand kit, verify company.* spans in ClickHouse
	cd $(PLATFORM_DIR) && ./scripts/verify-company-live.sh

hooks-install:
	@hooks_path=$$(git config --get core.hooksPath || true); \
	if [ "$$hooks_path" = "$(CURDIR)/.git/hooks" ] || [ "$$hooks_path" = ".git/hooks" ]; then \
		git config --unset-all core.hooksPath; \
	fi
	pre-commit install

fmt:
	gofumpt -w $(GO_DIRS)

vet:
	@set -e; for dir in $(GO_DIRS); do echo "==> $$dir"; (cd "$$dir" && go vet ./...); done

tidy:
	cd $(EC) && go mod tidy
	cd $(HS) && go mod tidy
	cd $(AW) && go mod tidy
	cd $(VMO) && go mod tidy
	cd $(BS) && go mod tidy
	cd $(GS) && go mod tidy
	cd $(IS) && go mod tidy
	cd $(SS) && go mod tidy
	cd $(SCH) && go mod tidy
	cd $(AM) && go mod tidy
	cd $(SR) && go mod tidy
	cd $(MS) && go mod tidy
	cd $(OSS) && go mod tidy
	cd $(PS) && go mod tidy
	cd $(NS) && go mod tidy
	cd $(PJS) && go mod tidy
	cd $(OT) && go mod tidy
	cd $(TP) && go mod tidy
	cd src/viteplus-monorepo && vp fmt . --write

sqlc: ## Regenerate sqlc stores for every service with sqlc.yaml
	@test -n "$(SQLC_DIRS)" || { echo "ERROR: no sqlc.yaml files found"; exit 1; }
	@for dir in $(SQLC_DIRS); do \
		echo "sqlc generate $$dir"; \
		(cd "$$dir" && sqlc generate); \
	done

sqlc-check: ## Verify committed sqlc generated stores are up to date
	@test -n "$(SQLC_DIRS)" || { echo "ERROR: no sqlc.yaml files found"; exit 1; }
	@for dir in $(SQLC_DIRS); do \
		echo "sqlc compile $$dir"; \
		(cd "$$dir" && sqlc compile); \
		echo "sqlc vet $$dir"; \
		(cd "$$dir" && sqlc vet); \
	done
	$(MAKE) sqlc
	@generated_files="$$(find $(SQLC_DIRS) -path '*/internal/store/*.go' -print | sort)"; \
		test -n "$$generated_files" || { echo "ERROR: no sqlc generated files found"; exit 1; }; \
		git diff --exit-code -- $$generated_files; \
		untracked="$$(git ls-files --others --exclude-standard -- $$generated_files)"; \
		test -z "$$untracked" || { echo "ERROR: untracked sqlc generated files:"; echo "$$untracked"; exit 1; }

openapi: ## Regenerate committed OpenAPI 3.0 and 3.1 specs for Go services
	go run ./$(BS)/cmd/billing-openapi --format 3.0 > $(BS)/openapi/openapi-3.0.yaml
	go run ./$(BS)/cmd/billing-openapi --format 3.1 > $(BS)/openapi/openapi-3.1.yaml
	mkdir -p $(GS)/openapi
	go run ./$(GS)/cmd/governance-openapi --format 3.0 > $(GS)/openapi/openapi-3.0.yaml
	go run ./$(GS)/cmd/governance-openapi --format 3.1 > $(GS)/openapi/openapi-3.1.yaml
	go run ./$(GS)/cmd/governance-internal-openapi --format 3.0 > $(GS)/openapi/internal-openapi-3.0.yaml
	go run ./$(GS)/cmd/governance-internal-openapi --format 3.1 > $(GS)/openapi/internal-openapi-3.1.yaml
	mkdir -p $(IS)/openapi
	go run ./$(IS)/cmd/identity-openapi --format 3.0 > $(IS)/openapi/openapi-3.0.yaml
	go run ./$(IS)/cmd/identity-openapi --format 3.1 > $(IS)/openapi/openapi-3.1.yaml
	go run ./$(IS)/cmd/identity-internal-openapi --format 3.0 > $(IS)/openapi/internal-openapi-3.0.yaml
	go run ./$(IS)/cmd/identity-internal-openapi --format 3.1 > $(IS)/openapi/internal-openapi-3.1.yaml
	mkdir -p $(SS)/openapi
	go run ./$(SS)/cmd/secrets-openapi --format 3.0 > $(SS)/openapi/openapi-3.0.yaml
	go run ./$(SS)/cmd/secrets-openapi --format 3.1 > $(SS)/openapi/openapi-3.1.yaml
	go run ./$(SS)/cmd/secrets-internal-openapi --format 3.0 > $(SS)/openapi/internal-openapi-3.0.yaml
	go run ./$(SS)/cmd/secrets-internal-openapi --format 3.1 > $(SS)/openapi/internal-openapi-3.1.yaml
	mkdir -p $(SCH)/openapi
	go run ./$(SCH)/cmd/source-code-hosting-openapi --format 3.0 > $(SCH)/openapi/openapi-3.0.yaml
	go run ./$(SCH)/cmd/source-code-hosting-openapi --format 3.1 > $(SCH)/openapi/openapi-3.1.yaml
	go run ./$(SCH)/cmd/source-code-hosting-internal-openapi --format 3.0 > $(SCH)/openapi/internal-openapi-3.0.yaml
	go run ./$(SCH)/cmd/source-code-hosting-internal-openapi --format 3.1 > $(SCH)/openapi/internal-openapi-3.1.yaml
	go run ./$(MS)/cmd/mailbox-openapi --format 3.0 > $(MS)/openapi/openapi-3.0.yaml
	go run ./$(MS)/cmd/mailbox-openapi --format 3.1 > $(MS)/openapi/openapi-3.1.yaml
	mkdir -p $(OSS)/openapi
	go run ./$(OSS)/cmd/object-storage-openapi --format 3.0 > $(OSS)/openapi/openapi-3.0.yaml
	go run ./$(OSS)/cmd/object-storage-openapi --format 3.1 > $(OSS)/openapi/openapi-3.1.yaml
	mkdir -p $(PS)/openapi
	go run ./$(PS)/cmd/profile-openapi --format 3.0 > $(PS)/openapi/openapi-3.0.yaml
	go run ./$(PS)/cmd/profile-openapi --format 3.1 > $(PS)/openapi/openapi-3.1.yaml
	go run ./$(PS)/cmd/profile-internal-openapi --format 3.0 > $(PS)/openapi/internal-openapi-3.0.yaml
	go run ./$(PS)/cmd/profile-internal-openapi --format 3.1 > $(PS)/openapi/internal-openapi-3.1.yaml
	mkdir -p $(NS)/openapi
	go run ./$(NS)/cmd/notifications-openapi --format 3.0 > $(NS)/openapi/openapi-3.0.yaml
	go run ./$(NS)/cmd/notifications-openapi --format 3.1 > $(NS)/openapi/openapi-3.1.yaml
	mkdir -p $(PJS)/openapi
	go run ./$(PJS)/cmd/projects-openapi --format 3.0 > $(PJS)/openapi/openapi-3.0.yaml
	go run ./$(PJS)/cmd/projects-openapi --format 3.1 > $(PJS)/openapi/openapi-3.1.yaml
	go run ./$(PJS)/cmd/projects-internal-openapi --format 3.0 > $(PJS)/openapi/internal-openapi-3.0.yaml
	go run ./$(PJS)/cmd/projects-internal-openapi --format 3.1 > $(PJS)/openapi/internal-openapi-3.1.yaml
	mkdir -p $(SR)/openapi
	go run ./$(SR)/cmd/sandbox-rental-openapi --format 3.0 > $(SR)/openapi/openapi-3.0.yaml
	go run ./$(SR)/cmd/sandbox-rental-openapi --format 3.1 > $(SR)/openapi/openapi-3.1.yaml
	go run ./$(SR)/cmd/sandbox-rental-internal-openapi --format 3.0 > $(SR)/openapi/internal-openapi-3.0.yaml
	go run ./$(SR)/cmd/sandbox-rental-internal-openapi --format 3.1 > $(SR)/openapi/internal-openapi-3.1.yaml
	$(MAKE) openapi-clients

openapi-clients: ## Regenerate committed generated Go clients from OpenAPI 3.0 specs
	cd $(BS)/client && go generate ./...
	cd $(GS)/client && go generate ./...
	cd $(GS)/internalclient && go generate ./...
	cd $(IS)/client && go generate ./...
	cd $(IS)/internalclient && go generate ./...
	cd $(SS)/client && go generate ./...
	cd $(SS)/internalclient && go generate ./...
	cd $(SCH)/client && go generate ./...
	cd $(SCH)/internalclient && go generate ./...
	cd $(SR)/client && go generate ./...
	cd $(SR)/internalclient && go generate ./...
	cd $(MS)/client && go generate ./...
	cd $(OSS)/client && go generate ./...
	cd $(PS)/client && go generate ./...
	cd $(PS)/internalclient && go generate ./...
	cd $(NS)/client && go generate ./...
	cd $(PJS)/client && go generate ./...
	cd $(PJS)/internalclient && go generate ./...

openapi-check: ## Verify committed OpenAPI specs are up to date
	cd $(BS) && go run ./cmd/billing-openapi --format 3.0 --check
	cd $(BS) && go run ./cmd/billing-openapi --format 3.1 --check
	cd $(GS) && go run ./cmd/governance-openapi --format 3.0 --check
	cd $(GS) && go run ./cmd/governance-openapi --format 3.1 --check
	cd $(GS) && go run ./cmd/governance-internal-openapi --format 3.0 --check
	cd $(GS) && go run ./cmd/governance-internal-openapi --format 3.1 --check
	cd $(IS) && go run ./cmd/identity-openapi --format 3.0 --check
	cd $(IS) && go run ./cmd/identity-openapi --format 3.1 --check
	cd $(IS) && go run ./cmd/identity-internal-openapi --format 3.0 --check
	cd $(IS) && go run ./cmd/identity-internal-openapi --format 3.1 --check
	cd $(SS) && go run ./cmd/secrets-openapi --format 3.0 --check
	cd $(SS) && go run ./cmd/secrets-openapi --format 3.1 --check
	cd $(SS) && go run ./cmd/secrets-internal-openapi --format 3.0 --check
	cd $(SS) && go run ./cmd/secrets-internal-openapi --format 3.1 --check
	cd $(SCH) && go run ./cmd/source-code-hosting-openapi --format 3.0 --check
	cd $(SCH) && go run ./cmd/source-code-hosting-openapi --format 3.1 --check
	cd $(SCH) && go run ./cmd/source-code-hosting-internal-openapi --format 3.0 --check
	cd $(SCH) && go run ./cmd/source-code-hosting-internal-openapi --format 3.1 --check
	cd $(MS) && go run ./cmd/mailbox-openapi --format 3.0 --check
	cd $(MS) && go run ./cmd/mailbox-openapi --format 3.1 --check
	cd $(OSS) && go run ./cmd/object-storage-openapi --format 3.0 --check
	cd $(OSS) && go run ./cmd/object-storage-openapi --format 3.1 --check
	cd $(PS) && go run ./cmd/profile-openapi --format 3.0 --check
	cd $(PS) && go run ./cmd/profile-openapi --format 3.1 --check
	cd $(PS) && go run ./cmd/profile-internal-openapi --format 3.0 --check
	cd $(PS) && go run ./cmd/profile-internal-openapi --format 3.1 --check
	cd $(NS) && go run ./cmd/notifications-openapi --format 3.0 --check
	cd $(NS) && go run ./cmd/notifications-openapi --format 3.1 --check
	cd $(PJS) && go run ./cmd/projects-openapi --format 3.0 --check
	cd $(PJS) && go run ./cmd/projects-openapi --format 3.1 --check
	cd $(PJS) && go run ./cmd/projects-internal-openapi --format 3.0 --check
	cd $(PJS) && go run ./cmd/projects-internal-openapi --format 3.1 --check
	cd $(SR) && go run ./cmd/sandbox-rental-openapi --format 3.0 --check
	cd $(SR) && go run ./cmd/sandbox-rental-openapi --format 3.1 --check
	cd $(SR) && go run ./cmd/sandbox-rental-internal-openapi --format 3.0 --check
	cd $(SR) && go run ./cmd/sandbox-rental-internal-openapi --format 3.1 --check
	$(MAKE) openapi-clients-check
	$(MAKE) openapi-wire-check

openapi-clients-check: ## Verify committed generated Go clients are up to date
	$(MAKE) openapi-clients
	git diff --exit-code -- $(GO_CLIENT_FILES)
	! rg -n "http\\.NewRequestWithContext\\(|\\.Do\\(req\\)|json\\.NewDecoder\\(|json\\.Marshal\\(" $(GO_CLIENT_DIRS) --glob '!**/client.gen.go' --glob '!**/generate.go'

openapi-wire-check: ## Verify frontend-consumed OpenAPI 3.1 specs are JS wire-safe
	go run ./$(AW)/cmd/openapi-wire-check \
		$(BS)/openapi/openapi-3.1.yaml \
		$(GS)/openapi/openapi-3.1.yaml \
		$(IS)/openapi/openapi-3.1.yaml \
		$(IS)/openapi/internal-openapi-3.1.yaml \
		$(SS)/openapi/openapi-3.1.yaml \
		$(SCH)/openapi/openapi-3.1.yaml \
		$(SCH)/openapi/internal-openapi-3.1.yaml \
		$(MS)/openapi/openapi-3.1.yaml \
		$(OSS)/openapi/openapi-3.1.yaml \
		$(PS)/openapi/openapi-3.1.yaml \
		$(PS)/openapi/internal-openapi-3.1.yaml \
		$(NS)/openapi/openapi-3.1.yaml \
		$(PJS)/openapi/openapi-3.1.yaml \
		$(PJS)/openapi/internal-openapi-3.1.yaml \
		$(SR)/openapi/openapi-3.1.yaml \
		$(SR)/openapi/internal-openapi-3.1.yaml

topology-generate: ## Regenerate Ansible deploy inputs from CUE topology
	cd $(PLATFORM_DIR) && ./scripts/topology.py generate

topology-check: ## Verify generated deploy inputs match CUE topology
	cd $(PLATFORM_DIR) && ./scripts/topology.py check

topology-proof: inventory-check ## Prove topology compile/check spans and generated artifact freshness in ClickHouse
	cd $(PLATFORM_DIR) && ./scripts/verify-topology-live.sh

inventory-check: ## Validate that the generated Ansible inventory exists
	@test -f "$(INVENTORY)" || { echo "ERROR: $(INVENTORY) not found. Run: cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/provision.yml"; exit 1; }

setup-dev: ## Install pinned controller dev tools
	cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/setup-dev.yml

setup-sops: ## Bootstrap SOPS + Age encryption
	cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/setup-sops.yml

provision: ## Provision bare metal and generate inventory
	cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/provision.yml

deprovision: ## Destroy provisioned bare metal infrastructure: make deprovision CONFIRM=deprovision
	@test "$(CONFIRM)" = "deprovision" || { echo "ERROR: deprovision requires CONFIRM=deprovision"; exit 1; }
	cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/deprovision.yml

deploy: inventory-check ## Deploy current site topology: make deploy [TAGS=billing_service,caddy]
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/site.yml $(if $(TAGS),--tags "$(TAGS)",)

guest-rootfs: inventory-check ## Build and stage Firecracker guest artifacts
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/guest-rootfs.yml $(if $(TAGS),--tags "$(TAGS)",)

security-patch: inventory-check ## Apply OS security updates through Ansible
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/security-patch.yml

identity-reset: inventory-check ## Exhaustively wipe identity-service PostgreSQL state and restart dependents
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/identity-reset.yml

seed-system: inventory-check ## Seed platform + Acme tenants, billing, mailboxes, and auth verify
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/seed-system.yml

assume-persona: inventory-check ## Useful utility: write persona env file: make assume-persona PERSONA=platform-admin [OUTPUT=path] [PRINT=1]
	@test -n "$(PERSONA)" || { echo "ERROR: PERSONA is required (platform-admin, acme-admin, acme-member)"; exit 1; }
	@cd $(PLATFORM_DIR) && ./scripts/assume-persona.sh "$(PERSONA)" $(ASSUME_PERSONA_FLAGS)

assume-platform-admin: inventory-check ## Useful utility: write env for platform admin agent persona
	@cd $(PLATFORM_DIR) && ./scripts/assume-persona.sh platform-admin $(ASSUME_PERSONA_FLAGS)

assume-acme-admin: inventory-check ## Useful utility: write env for Acme org admin persona
	@cd $(PLATFORM_DIR) && ./scripts/assume-persona.sh acme-admin $(ASSUME_PERSONA_FLAGS)

assume-acme-member: inventory-check ## Useful utility: write env for Acme org member persona
	@cd $(PLATFORM_DIR) && ./scripts/assume-persona.sh acme-member $(ASSUME_PERSONA_FLAGS)

set-user-state: inventory-check ## Set billing fixture state: make set-user-state EMAIL=ceo@example.com ORG=platform STATE=pro [BALANCE_CENTS=10000]
	@cd $(PLATFORM_DIR) && ./scripts/set-user-state.sh \
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
	@cd $(PLATFORM_DIR) && ./scripts/billing-clock.sh \
		--org "$(ORG)" \
		--org-id "$(ORG_ID)" \
		--product-id "$(BILLING_PRODUCT_ID)" \
		$(if $(SET),--set "$(SET)",) \
		$(if $(ADVANCE_SECONDS),--advance-seconds "$(ADVANCE_SECONDS)",) \
		$(if $(CLEAR),--clear,) \
		$(if $(REASON),--reason "$(REASON)",)

billing-wall-clock: inventory-check ## Reset billing test time to wall-clock and repair current cycle: make billing-wall-clock ORG=platform
	@cd $(PLATFORM_DIR) && ./scripts/billing-clock.sh \
		--org "$(ORG)" \
		--org-id "$(ORG_ID)" \
		--product-id "$(BILLING_PRODUCT_ID)" \
		--wall-clock \
		$(if $(REASON),--reason "$(REASON)",)

billing-state: inventory-check ## Inspect billing state for an org: make billing-state ORG=platform [BILLING_PRODUCT_ID=sandbox]
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/billing-inspect.sh --kind state --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-documents: inventory-check ## List billing documents for an org: make billing-documents ORG=platform
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/billing-inspect.sh --kind documents --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-finalizations: inventory-check ## List billing finalizations for an org: make billing-finalizations ORG=platform
	@test -n "$(ORG)$(ORG_ID)" || { echo "ERROR: ORG or ORG_ID is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/billing-inspect.sh --kind finalizations --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(FORMAT),--format "$(FORMAT)",)

billing-events: inventory-check ## Query recent billing events in ClickHouse: make billing-events [ORG_ID=123] [EVENT=billing_document_issued] [MINUTES=60]
	cd $(PLATFORM_DIR) && ./scripts/billing-inspect.sh --kind events --org "$(ORG)" --org-id "$(ORG_ID)" --product-id "$(BILLING_PRODUCT_ID)" $(if $(EVENT),--event-type "$(EVENT)",) $(if $(MINUTES),--minutes "$(MINUTES)",) $(if $(FORMAT),--format "$(FORMAT)",)

billing-pg-shell: inventory-check ## Open psql against the billing database
	cd $(PLATFORM_DIR) && ./scripts/pg.sh billing

billing-pg-query: inventory-check ## Run a PostgreSQL query against billing: make billing-pg-query QUERY='SELECT 1'
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/pg.sh billing --query "$(QUERY)"

billing-proof: inventory-check ## Run live billing browser proof and collect evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-console-billing-flow.sh

profile-proof: inventory-check ## Run live profile API/UI proof and assert PostgreSQL plus ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-profile-live.sh

organization-sync-proof: inventory-check ## Run live organization auto-sync/OCC proof and assert PostgreSQL plus ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-organization-sync-live.sh

notifications-proof: inventory-check ## Run live notifications bell proof and assert PostgreSQL plus ClickHouse traces
	cd $(PLATFORM_DIR) && ./scripts/verify-notifications-live.sh

projects-proof: inventory-check ## Run live projects API proof and assert PostgreSQL plus ClickHouse traces
	cd $(PLATFORM_DIR) && ./scripts/verify-projects-live.sh

source-code-hosting-proof: inventory-check ## Run live source repository UI/API proof and assert PostgreSQL plus ClickHouse traces
	cd $(PLATFORM_DIR) && ./scripts/verify-source-code-hosting-live.sh

secrets-proof: inventory-check ## Run live secrets API proof and collect audit/trace evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-secrets-live.sh

secrets-leak-proof: inventory-check ## Prove bearer/JWT material is absent from traces, logs, audit rows, and proof artifacts
	cd $(PLATFORM_DIR) && ./scripts/verify-secrets-leak-proof.sh

openbao-proof: inventory-check ## Prove OpenBao process, health, metrics, audit log, nftables, and ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-openbao-live.sh

openbao-tenancy-proof: inventory-check ## Prove OpenBao per-org mounts, JWT roles, SPIFFE workload roles, policies, and ClickHouse spans
	cd $(PLATFORM_DIR) && ./scripts/verify-openbao-tenancy-live.sh

object-storage-verify: inventory-check ## Verify the Garage-backed object-storage runtime and assert ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-object-storage-live.sh

workload-identity-proof: inventory-check ## Prove SPIFFE mTLS/JWT-SVID boundaries, SPIRE bundle JWKS, stale credential deletion, and ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-workload-identity-live.sh

spiffe-rotation-proof: inventory-check ## Prove file-backed SPIFFE consumers reload rotated material and assert ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-spiffe-rotation-live.sh

temporal-verify: inventory-check ## Verify the Temporal runtime, bootstrap path, and ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-temporal-live.sh

temporal-web-proof: inventory-check ## Verify Temporal Web login, operator routing, and ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-temporal-web-live.sh

recurring-schedule-proof: inventory-check ## Create a paused Temporal-backed recurring schedule, resume it, and assert PostgreSQL + ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-recurring-schedule-live.sh

billing-reset: inventory-check ## Exhaustively wipe billing state (TigerBeetle + billing PostgreSQL schema) and restart billing callers
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/billing-reset.yml

verification-reset: inventory-check ## Exhaustively wipe verification state (billing, sandbox_rental, ClickHouse verself + telemetry)
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/verification-reset.yml

wipe-pg-db: inventory-check ## Wipe one managed PostgreSQL service DB: make wipe-pg-db DB=sandbox_rental
	@test -n "$(DB)" || { echo "ERROR: DB is required (billing|sandbox_rental|mailbox_service|identity_service|secrets_service|notifications_service|projects_service|source_code_hosting)"; exit 1; }
	$(PLATFORM_DIR)/scripts/ansible-with-tunnel.sh playbooks/wipe-pg-db.yml -e "wipe_pg_db_name=$(DB)"

vm-orchestrator-proof: inventory-check ## Live proof for vm-orchestrator lease/exec spans through recurring sandbox executions
	cd $(PLATFORM_DIR) && ./scripts/verify-vm-orchestrator-live.sh

sandbox-inner: inventory-check ## Inner loop: default starts local HMR; use SANDBOX_INNER_MODE=verify for local smoke evidence
	cd $(PLATFORM_DIR) && ./scripts/sandbox-inner.sh

sandbox-middle: inventory-check ## Middle loop: default deploys UI and runs admin smoke; use SANDBOX_DEPLOY_TARGET=ui|service|both|none SANDBOX_VERIFY_TARGET=admin|schedule|billing|none SANDBOX_SEED_VERIFY=1
	cd $(PLATFORM_DIR) && ./scripts/sandbox-middle.sh

sandbox-proof: inventory-check ## Proof loop: full reset, redeploy, reseed, and live full-lifecycle sandbox verification
	cd $(PLATFORM_DIR) && ./scripts/verify-sandbox-live.sh

console-ui-smoke: inventory-check ## Run deployed console authenticated shell smoke
	cd $(PLATFORM_DIR) && TEST_BASE_URL="$${TEST_BASE_URL:-https://console.$$(awk -F'\"' '/^verself_domain:/{print $$2}' ansible/group_vars/all/main.yml)}" ./scripts/verify-console-ui-smoke.sh

console-ui-local: inventory-check ## Run console smoke against local HMR dev server
	cd $(PLATFORM_DIR) && ./scripts/verify-console-ui-local.sh

console-local-dev: inventory-check ## Start local console dev tunnels and HMR server
	cd $(PLATFORM_DIR) && ./scripts/run-console-local-dev.sh $(if $(PRINT_ENV),--print-env,)

console-frontend-deploy-fast: inventory-check ## Ship UI-only changes to console: local build + rsync .output/ + restart (~5-10s). For API/env/systemd/OIDC changes use `ansible-playbook ... --tags console`.
	$(PLATFORM_DIR)/scripts/console-frontend-deploy-fast.sh

platform-frontend-deploy-fast: inventory-check ## Ship UI-only changes to platform docs: local build + rsync .output/ + restart (~5-10s). For env/systemd/nftables/Caddy changes use `ansible-playbook ... --tags platform`.
	$(PLATFORM_DIR)/scripts/platform-frontend-deploy-fast.sh

platform-local-dev: ## Start local platform docs HMR dev server (no tunnels; no service deps)
	cd src/viteplus-monorepo/apps/platform && VERSELF_DOMAIN=$$(awk -F'"' '/^verself_domain:/{print $$2}' $(PLATFORM_DIR)/ansible/group_vars/all/main.yml) PRODUCT_BASE_URL=https://$$(awk -F'"' '/^verself_domain:/{print $$2}' $(PLATFORM_DIR)/ansible/group_vars/all/main.yml) BASE_URL=https://$$(awk -F'"' '/^verself_domain:/{print $$2}' $(PLATFORM_DIR)/ansible/group_vars/all/main.yml) vp dev

grafana-proof: inventory-check ## Verify Grafana health, datasource execution, PostgreSQL state, and ClickHouse evidence
	cd $(PLATFORM_DIR) && ./scripts/verify-grafana-live.sh

services-doctor: inventory-check ## Cross-check generated topology endpoints against live listeners: make services-doctor [FORMAT=table|json|nftables]
	@python3 $(PLATFORM_DIR)/scripts/services-doctor.py

observe: inventory-check ## Discover/query telemetry: make observe [WHAT=catalog|queries|describe|metric|trace|logs|http|service|errors|mail|deploy|workload-identity|temporal] [SIGNAL=...] [FORMAT=table|json|markdown]
	cd $(PLATFORM_DIR) && ./scripts/observe.sh $(if $(WHAT),--what "$(WHAT)",) $(if $(SIGNAL),--signal "$(SIGNAL)",) $(if $(SERVICE),--service "$(SERVICE)",) $(if $(METRIC),--metric "$(METRIC)",) $(if $(SPAN),--span "$(SPAN)",) $(if $(FIELD),--field "$(FIELD)",) $(if $(QUERY),--query "$(QUERY)",) $(if $(PREFIX),--prefix "$(PREFIX)",) $(if $(SEARCH),--search "$(SEARCH)",) $(if $(GROUP_BY),--group-by "$(GROUP_BY)",) $(if $(MODE),--mode "$(MODE)",) $(if $(TRACE_ID),--trace-id "$(TRACE_ID)",) $(if $(RUN_KEY),--run-key "$(RUN_KEY)",) $(if $(HOST),--host "$(HOST)",) $(if $(STATUS_MIN),--status-min "$(STATUS_MIN)",) $(if $(FORMAT),--format "$(FORMAT)",) $(if $(MINUTES),--minutes "$(MINUTES)",) $(if $(LIMIT),--limit "$(LIMIT)",) $(if $(ERRORS),--errors,)

telemetry-proof: inventory-check ## Run observability smoke and verify ansible spans land in ClickHouse
	cd $(PLATFORM_DIR) && ./scripts/telemetry-proof.sh

telemetry-proof-fail: inventory-check ## Run observability smoke fail-path and verify Error spans land in ClickHouse
	cd $(PLATFORM_DIR) && TELEMETRY_PROOF_EXPECT_FAIL=1 ./scripts/telemetry-proof.sh

observability-smoke: inventory-check ## Run the raw Ansible observability smoke playbook
	cd $(PLATFORM_DIR)/ansible && ansible-playbook playbooks/observability-smoke.yml

clickhouse-query: inventory-check ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=verself]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

clickhouse-schemas: inventory-check ## Print CREATE TABLE statements for all project tables
	cd $(PLATFORM_DIR) && ./scripts/clickhouse.sh --query "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('verself', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw"

pg-list: inventory-check ## List PostgreSQL databases on the worker (authoritative via \l)
	cd $(PLATFORM_DIR) && ./scripts/pg.sh --list

pg-shell: inventory-check ## Open interactive psql: make pg-shell DB=sandbox_rental (run 'make pg-list' to see databases)
	@test -n "$(DB)" || { echo "ERROR: DB is required (run 'make pg-list' to see databases)"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/pg.sh "$(DB)"

pg-query: inventory-check ## Run a PostgreSQL query on the worker: make pg-query DB=sandbox_rental QUERY='SELECT 1'
	@test -n "$(DB)" || { echo "ERROR: DB is required (run 'make pg-list' to see databases)"; exit 1; }
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/pg.sh "$(DB)" --query "$(QUERY)"

tb-shell: inventory-check ## Open the TigerBeetle REPL (Ctrl+D to exit). Ops: create_accounts, create_transfers, lookup_accounts, lookup_transfers, get_account_transfers, get_account_balances, query_accounts, query_transfers
	cd $(PLATFORM_DIR) && ./scripts/tigerbeetle.sh

tb-command: inventory-check ## Run a single TigerBeetle REPL op: make tb-command COMMAND='query_accounts limit=10;'
	@test -n "$(COMMAND)" || { echo "ERROR: COMMAND is required (e.g. 'lookup_accounts id=1;')"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/tigerbeetle.sh --command "$(COMMAND)"

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
	cd $(PLATFORM_DIR) && ./scripts/mail-send.sh -t "$(TO)" -s "$(SUBJECT)" -b "$(BODY)"

mail-send-agents: inventory-check ## Send via Resend to agents inbox: make mail-send-agents SUBJECT='test' BODY='hello'
	@test -n "$(SUBJECT)" || { echo "ERROR: SUBJECT is required"; exit 1; }
	@test -n "$(BODY)" || { echo "ERROR: BODY is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/mail-send.sh -t agents -s "$(SUBJECT)" -b "$(BODY)"

mail-send-ceo: inventory-check ## Send via Resend to ceo inbox: make mail-send-ceo SUBJECT='test' BODY='hello'
	@test -n "$(SUBJECT)" || { echo "ERROR: SUBJECT is required"; exit 1; }
	@test -n "$(BODY)" || { echo "ERROR: BODY is required"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/mail-send.sh -t ceo -s "$(SUBJECT)" -b "$(BODY)"

mail-passwords: inventory-check ## Show Stalwart mailbox passwords
	@echo "ceo@$$(cd $(PLATFORM_DIR) && grep '^verself_domain:' ansible/group_vars/all/main.yml | awk '{print $$2}' | tr -d '\"'):"
	@cd $(PLATFORM_DIR) && sops -d --extract '["stalwart_ceo_password"]' ansible/group_vars/all/secrets.sops.yml
	@echo ""
	@echo "agents@$$(cd $(PLATFORM_DIR) && grep '^verself_domain:' ansible/group_vars/all/main.yml | awk '{print $$2}' | tr -d '\"'):"
	@cd $(PLATFORM_DIR) && sops -d --extract '["stalwart_agents_password"]' ansible/group_vars/all/secrets.sops.yml

edit-secrets: ## Open encrypted secrets in $$EDITOR via sops
	sops $(PLATFORM_DIR)/ansible/group_vars/all/secrets.sops.yml

wipe-server: inventory-check ## Wipe all verself state from the provisioned server: make wipe-server CONFIRM=wipe-server
	@test "$(CONFIRM)" = "wipe-server" || { echo "ERROR: wipe-server requires CONFIRM=wipe-server"; exit 1; }
	cd $(PLATFORM_DIR) && ./scripts/wipe-server.sh
