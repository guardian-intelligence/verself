.PHONY: build clean test test-integration lint lint-ansible fmt vet tidy \
       hooks-install doctor setup-domain \
       server-profile smelter-build \
       clickhouse-shell clickhouse-query clickhouse-schemas edit-secrets

BINARY   := forge-metal
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

build: ## Build the forge-metal Go binary
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

setup-domain: build ## Configure Cloudflare domain (interactive wizard)
	./$(BINARY) setup-domain $(DOMAIN)

server-profile: ## Build Nix server profile (golden image closure)
	nix build .#server-profile --print-out-paths

clickhouse-shell: ## Open an interactive clickhouse-client session on the worker
	./scripts/clickhouse.sh

clickhouse-query: ## Run a ClickHouse query on the worker: make clickhouse-query QUERY='SHOW TABLES' [DATABASE=forge_metal]
	@test -n "$(QUERY)" || { echo "ERROR: QUERY is required"; exit 1; }
	./scripts/clickhouse.sh $(if $(DATABASE),--database $(DATABASE),) --query "$(QUERY)"

clickhouse-schemas: ## Print CREATE TABLE statements for all project tables
	./scripts/clickhouse.sh --query "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('forge_metal', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw"

edit-secrets: ## Open encrypted secrets in $$EDITOR via sops
	sops ansible/group_vars/all/secrets.sops.yml
