.PHONY: build clean test lint fmt vet tidy proto \
       setup-sops edit-secrets provision nuke-and-pave

BINARY := bmci
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

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

proto:
	buf generate internal/proto

setup-sops: ## Generate age key, encrypt initial secrets, install sops collection
	./scripts/setup-sops.sh

edit-secrets: ## Open encrypted secrets in $EDITOR via sops
	sops ansible/group_vars/all/secrets.sops.yml

provision: ## Run full site.yml provision (no wipe)
	cd ansible && ansible-playbook playbooks/site.yml
