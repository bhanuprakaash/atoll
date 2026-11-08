.PHONY: check fmt format lint vet typecheck test run e2e ui verify app icon

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
	go test -shuffle=on -timeout=60s $(filter-out $(CURDIR)/e2e,$(GO_DIRS))

run: ui
	go run ./cmd/atoll serve

app: ui
	mkdir -p desktop/src-tauri/binaries
	go build -o desktop/src-tauri/binaries/atoll-$$(rustc -vV | sed -n "s/^host: //p") ./cmd/atoll
	cd desktop && $(PNPM) install --frozen-lockfile && $(PNPM) tauri build --bundles app,dmg

# Compiles the layered icon (icons/icon.icon, Icon Composer) into the committed Assets.car. Run it
# after changing the icon: actool fails with "Bad file descriptor" when the bundler runs it in CI.
icon:
	@d=$$(mktemp -d) && xcrun actool desktop/src-tauri/icons/icon.icon --compile $$d \
		--output-partial-info-plist $$d/info.plist --app-icon Icon --include-all-app-icons \
		--enable-on-demand-resources NO --development-region en --target-device mac \
		--minimum-deployment-target 10.13 --platform macosx >/dev/null && \
		cp $$d/Assets.car desktop/src-tauri/icons/Assets.car && rm -rf $$d

# The binary embeds server/ui, built from web/.
ui: server/ui/index.html

server/ui/index.html: web/node_modules $(shell find web/src -type f) $(wildcard web/*.html web/*.ts web/*.json)
	cd web && $(PNPM) run build
	@touch server/ui/.keep

e2e: ui e2e/ui/node_modules
	@status=0; \
	e2e/with-secret-store go test -count=1 -timeout=15m ./e2e || status=1; \
	(cd e2e/ui && $(PNPM) exec playwright test) || status=1; \
	exit $$status

e2e/ui/node_modules: e2e/ui/pnpm-lock.yaml
	cd e2e/ui && $(PNPM) install --frozen-lockfile && $(PNPM) exec playwright install chromium
	@touch $@

# Real server, API and UI; proof lands in .verify/. ARGS: --fake, --provider, --out.
verify: ui scripts/verify/node_modules
	node scripts/verify/verify.mjs $(ARGS)

scripts/verify/node_modules: scripts/verify/pnpm-lock.yaml
	cd scripts/verify && $(PNPM) install --frozen-lockfile && $(PNPM) exec playwright install chromium
	@touch $@

web/node_modules: web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile
	@touch $@
