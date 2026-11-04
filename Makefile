.PHONY: check fmt format lint vet typecheck test run e2e

# web/node_modules ships Go files; keep them out of every Go command.
GO_DIRS = $(shell go list -f '{{.Dir}}' ./... | grep -v /node_modules/)

PNPM_VERSION = $(shell sed -n 's/.*"packageManager": "pnpm@\([^"]*\)".*/\1/p' web/package.json)
PNPM = $(if $(shell command -v pnpm),pnpm,$(error pnpm not found; install it with: npm i -g pnpm@$(PNPM_VERSION)))

check: fmt lint vet typecheck test

fmt: web/node_modules
	@files=$$(gofmt -l $(GO_DIRS)); [ -z "$$files" ] || { echo "not gofmt-ed: $$files"; exit 1; }
	cd web && $(PNPM) exec prettier --check .

format: web/node_modules
	gofmt -w $(GO_DIRS)
	cd web && $(PNPM) exec prettier --write .

lint: web/node_modules
	go tool golangci-lint run $(GO_DIRS)
	cd web && $(PNPM) exec eslint .

vet:
	go vet $(GO_DIRS)

typecheck: web/node_modules
	cd web && $(PNPM) exec tsc --noEmit

test:
	go test $(GO_DIRS)

run:
	go run ./cmd/atoll

e2e:
	@echo "e2e: no suite yet"

web/node_modules: web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile
	@touch $@
