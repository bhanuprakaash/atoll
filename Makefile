.PHONY: check fmt format lint vet typecheck test run e2e

# web/node_modules ships Go files; keep them out of every Go command.
GO_DIRS = $(shell go list -f '{{.Dir}}' ./... | grep -v /node_modules/)

check: fmt lint vet typecheck test

fmt: web/node_modules
	@files=$$(gofmt -l $(GO_DIRS)); [ -z "$$files" ] || { echo "not gofmt-ed: $$files"; exit 1; }
	cd web && npx prettier --check .

format: web/node_modules
	gofmt -w $(GO_DIRS)
	cd web && npx prettier --write .

lint: web/node_modules
	go tool golangci-lint run $(GO_DIRS)
	cd web && npx eslint .

vet:
	go vet $(GO_DIRS)

typecheck: web/node_modules
	cd web && npx tsc --noEmit

test:
	go test $(GO_DIRS)

run:
	go run ./cmd/atoll

e2e:
	@echo "e2e: no suite yet"

web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@
