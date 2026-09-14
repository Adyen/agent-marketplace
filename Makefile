VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY := agent-marketplace
RELEASE_BINARY := $(BINARY)-$(VERSION)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
GO := go
CMD := ./cmd/agent-marketplace

# Public container defaults. Override these generic variables in the environment
# when builds must use a registry mirror.
GO_BUILD_IMAGE ?= golang:1.26.6-bookworm
RUNTIME_IMAGE ?= gcr.io/distroless/static-debian12:nonroot
NODE_IMAGE ?= node:24-alpine
POSTGRES_IMAGE ?= postgres:17.5-alpine
NGINX_IMAGE ?= nginx:1.27-alpine
MARKETPLACE_CONTAINER_IMAGE ?= agent-marketplace:$(VERSION)
WEB_CONTAINER_IMAGE ?= agent-marketplace-web:$(VERSION)
export GO_BUILD_IMAGE RUNTIME_IMAGE NODE_IMAGE POSTGRES_IMAGE NGINX_IMAGE

# Platform targets
PLATFORMS := darwin-arm64 darwin-amd64 linux-amd64 linux-arm64
DIST_DIR := dist

.PHONY: build test lint ci-lint lint-common tidy version release clean e2e marketplace-gate devin-probe tui-qa-check tui-qa tui-qa-contained site-dev site-build secrets-check container container-marketplace container-web container-check compose-config $(PLATFORMS)
LINT_BASE ?= origin/main

define run_golangci_lint
	@GOLANGCI_LINT_BIN="$$(command -v golangci-lint || true)"; \
	if [ -z "$$GOLANGCI_LINT_BIN" ]; then \
		FALLBACK_BIN="$$($(GO) env GOPATH)/bin/golangci-lint"; \
		if [ -x "$$FALLBACK_BIN" ]; then \
			GOLANGCI_LINT_BIN="$$FALLBACK_BIN"; \
		fi; \
	fi; \
	[ -n "$$GOLANGCI_LINT_BIN" ] || { echo "ERROR: golangci-lint not found. Install it to run local lint parity with CI."; exit 1; }; \
	"$$GOLANGCI_LINT_BIN" run $(GOLANGCI_LINT_ARGS)
endef

build:
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o $(BINARY) $(CMD)

test:
	@: # Tests shadow HOME per-package (TestMain) and per-test (t.Setenv), so
	@: # Makefile no longer needs a temporary HOME. Git environment is still
	@: # neutralized so tests that spawn git commands work when run from hooks.
	@set -e; \
	unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
		GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_COMMON_DIR GIT_CONFIG_PARAMETERS; \
	GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_COUNT=1 \
		GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=0 $(GO) test ./...

lint: LINT_MODE=fix
lint: lint-common

ci-lint: LINT_MODE=check
ci-lint: GOLANGCI_LINT_ARGS=--new-from-rev $(LINT_BASE)
ci-lint: lint-common

lint-common:
	@if [ "$(LINT_MODE)" = "fix" ]; then \
		$(MAKE) tidy; \
		$(GO) fmt ./...; \
	else \
		GOWORK=off $(GO) mod tidy -diff; \
		test -z "$$(gofmt -l ./cmd ./internal)" || { echo "ERROR: gofmt found formatting issues - please run 'make lint' and commit the results"; gofmt -d ./cmd ./internal; exit 1; }; \
	fi
	$(GO) vet ./...
	$(run_golangci_lint)

# Every Go module in the repository. `make tidy` refreshes each module's
# go.mod/go.sum in module mode (GOWORK=off). The committed go.work workspace
# already lets CI build across modules without cross-module tidy, so these
# targets are hygiene: a stale e2e/go.mod never blocks a dependency bump.
GO_MODULES := . e2e

tidy:
	@set -e; for module in $(GO_MODULES); do \
		GOWORK=off $(GO) -C "$$module" mod tidy; \
	done

version:
	@echo $(VERSION)

container: container-marketplace container-web

container-marketplace:
	docker build \
		--build-arg GO_BUILD_IMAGE="$(GO_BUILD_IMAGE)" \
		--build-arg RUNTIME_IMAGE="$(RUNTIME_IMAGE)" \
		--build-arg VERSION="$(VERSION)" \
		-t "$(MARKETPLACE_CONTAINER_IMAGE)" -f Dockerfile.marketplace .

container-web:
	docker build \
		--build-arg NODE_IMAGE="$(NODE_IMAGE)" \
		-t "$(WEB_CONTAINER_IMAGE)" web

secrets-check:
	@test -n "$$(git check-ignore --no-index deploy/compose/postgres-s3/.env 2>/dev/null)" || { echo "ERROR: deploy .env files must be ignored"; exit 1; }
	@test -z "$$(git ls-files | awk '/^deploy\/.*\/\.env$$/ {print}')" || { echo "ERROR: tracked deploy .env file"; exit 1; }
	@if git grep -nIE 'postgres(ql)?://[^[:space:]/:@]+:[^[:space:]@]+@|^(AWS_SECRET_ACCESS_KEY|AWS_SESSION_TOKEN|POSTGRES_PASSWORD|MARKETPLACE_POSTGRES_DSN)=|(^|[[:space:]])(password|token|access_token)[[:space:]]*=' -- deploy | grep -vE 'replace-with|example\.invalid|\.example(:|$$)|^[^:]+:[^:]+:#'; then \
		echo "ERROR: tracked deployment content looks like it contains a real secret"; exit 1; \
	fi
	@echo "Deployment secret static check passed"

container-check: secrets-check
	@test -s Dockerfile.marketplace
	@test -s .dockerignore
	@test -s web/Dockerfile
	@awk '/^ARG GO_BUILD_IMAGE=golang:1.26.6-bookworm$$/{goarg=1} /^ARG RUNTIME_IMAGE=gcr.io\/distroless\/static-debian12:nonroot$$/{runtimearg=1} /FROM .*\$$\{GO_BUILD_IMAGE\} AS build/{build=1} /FROM .*\$$\{GO_BUILD_IMAGE\} AS git-runtime/{gitstage=1} /-trimpath -buildvcs=false/{flags=1} /FROM \$$\{RUNTIME_IMAGE\} AS api/{api=1} /FROM \$$\{RUNTIME_IMAGE\} AS worker/{worker=1} /command -v git/{git=1} /command -v ssh/{ssh=1} /USER 1000:1000/{users++} /(apk|apt-get|dnf|yum) (add|install)/{packageadds++} END {if (!goarg || !runtimearg || !build || !gitstage || !flags || !api || !worker || !git || !ssh || packageadds != 0 || users < 2) exit 1}' Dockerfile.marketplace
	@awk '/^ARG NODE_IMAGE=node:24-alpine$$/{arg=1} /FROM \$$\{NODE_IMAGE\} AS build/{build=1} /FROM \$$\{NODE_IMAGE\} AS runtime/{runtime=1} /COPY package.json pnpm-lock.yaml pnpm-workspace.yaml/{workspace=1} /pnpm install --frozen-lockfile --ignore-scripts/{install=1} /USER 1000:1000/{user=1} /HEALTHCHECK/{health=1} END {if (!arg || !build || !runtime || !workspace || !install || !user || !health) exit 1}' web/Dockerfile
	@test "$$(grep -Fh -- 'GO_BUILD_IMAGE:' deploy/compose/postgres-s3/compose.yaml compose.yaml | wc -l | tr -d ' ')" -eq 2
	@test "$$(grep -Fh -- 'RUNTIME_IMAGE:' deploy/compose/postgres-s3/compose.yaml compose.yaml | wc -l | tr -d ' ')" -eq 2
	@test "$$(grep -Fh -- 'NODE_IMAGE:' deploy/compose/postgres-s3/compose.yaml compose.yaml | wc -l | tr -d ' ')" -eq 2
	@test "$$(grep -Fh -- 'NGINX_IMAGE' deploy/compose/postgres-s3/compose.yaml compose.yaml | wc -l | tr -d ' ')" -eq 2
	@test "$$(grep -Fc -- 'POSTGRES_IMAGE' deploy/compose/postgres-s3/compose.yaml)" -eq 1
	@test "$$(grep -Ec -- '^[[:space:]]*--build-arg GO_BUILD_IMAGE=' Makefile)" -eq 1
	@test "$$(grep -Ec -- '^[[:space:]]*--build-arg RUNTIME_IMAGE=' Makefile)" -eq 1
	@test "$$(grep -Ec -- '^[[:space:]]*--build-arg NODE_IMAGE=' Makefile)" -eq 1
	@awk '/^GO_BUILD_IMAGE \?= golang:1.26.6-bookworm$$/{go=1} /^RUNTIME_IMAGE \?= gcr.io\/distroless\/static-debian12:nonroot$$/{runtime=1} /^NODE_IMAGE \?= node:24-alpine$$/{node=1} /^POSTGRES_IMAGE \?= postgres:17.5-alpine$$/{postgres=1} /^NGINX_IMAGE \?= nginx:1.27-alpine$$/{nginx=1} END {if (!go || !runtime || !node || !postgres || !nginx) exit 1}' Makefile
	@if git grep -nE 'is\.adyen\.(com|cloud)' -- Dockerfile.marketplace web/Dockerfile deploy/compose ':!deploy/compose/postgres-s3/README.md'; then \
		echo "ERROR: tracked public container defaults contain an internal Adyen hostname; use an environment override instead"; exit 1; \
	fi
	@test "$$(grep -Fc -- 'CMD ["/app/marketplace-healthcheck"]' Dockerfile.marketplace)" -eq 1
	@test "$$(grep -Fh -- 'marketplace-healthcheck' deploy/compose/postgres-s3/compose.yaml compose.yaml | grep -Fc -- '["CMD", "/app/marketplace-healthcheck"]')" -eq 2
	@test -z "$$(grep -n -- 'wget' Dockerfile.marketplace deploy/compose/postgres-s3/compose.yaml compose.yaml)"
	@echo "Container static checks passed"

compose-config:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "SKIP: docker unavailable; cannot validate Compose files"; \
	elif ! docker compose version >/dev/null 2>&1; then \
		echo "SKIP: docker compose unavailable; cannot validate Compose files"; \
	else \
		secret_dir=$$(mktemp -d) || { echo "ERROR: cannot create temporary Compose secret directory"; exit 1; }; \
		secret_file=$$secret_dir/marketplace-postgres-dsn; \
		password_file=$$secret_dir/marketplace-postgres-password; \
		secret_override=$$(mktemp) || { rm -rf "$$secret_dir"; echo "ERROR: cannot create temporary Compose override"; exit 1; }; \
		sqlite_config=$$(mktemp) || { rm -rf "$$secret_dir"; rm -f "$$secret_override"; echo "ERROR: cannot create temporary Compose output"; exit 1; }; \
		postgres_config=$$(mktemp) || { rm -rf "$$secret_dir"; rm -f "$$secret_override" "$$sqlite_config"; echo "ERROR: cannot create temporary Compose output"; exit 1; }; \
		trap 'rm -rf "$$secret_dir"; rm -f "$$secret_override" "$$sqlite_config" "$$postgres_config"' EXIT; \
		printf '%s\n' 'postgres://marketplace:validation-only@postgres:5432/marketplace?sslmode=disable' > "$$secret_file"; \
		printf '%s\n' 'validation-only' > "$$password_file"; \
		printf '%s\n' 'secrets:' '  marketplace_postgres_password:' '    file: '"$$password_file" '  marketplace_postgres_dsn:' '    file: '"$$secret_file" > "$$secret_override"; \
		docker compose -f compose.yaml config --quiet && \
		docker compose -f deploy/compose/postgres-s3/compose.yaml -f "$$secret_override" config --quiet && \
		GO_BUILD_IMAGE=override/go:test RUNTIME_IMAGE=override/runtime:test NODE_IMAGE=override/node:test NGINX_IMAGE=override/nginx:test docker compose -f compose.yaml config > "$$sqlite_config" && \
		grep -Fq 'override/go:test' "$$sqlite_config" && grep -Fq 'override/runtime:test' "$$sqlite_config" && grep -Fq 'override/node:test' "$$sqlite_config" && grep -Fq 'override/nginx:test' "$$sqlite_config" && \
		GO_BUILD_IMAGE=override/go:test RUNTIME_IMAGE=override/runtime:test NODE_IMAGE=override/node:test POSTGRES_IMAGE=override/postgres:test NGINX_IMAGE=override/nginx:test docker compose -f deploy/compose/postgres-s3/compose.yaml -f "$$secret_override" config > "$$postgres_config" && \
		grep -Fq 'override/go:test' "$$postgres_config" && grep -Fq 'override/runtime:test' "$$postgres_config" && grep -Fq 'override/node:test' "$$postgres_config" && grep -Fq 'override/postgres:test' "$$postgres_config" && grep -Fq 'override/nginx:test' "$$postgres_config" && \
		test "$$(grep -Ec -- '^[[:space:]]+target: marketplace-postgres-password$$' "$$postgres_config")" -eq 1 && \
		test "$$(grep -Ec -- '^[[:space:]]+target: marketplace-postgres-dsn$$' "$$postgres_config")" -eq 2 && \
		test "$$(grep -Ec -- '^[[:space:]]+target: marketplace_postgres_(password|dsn)$$' "$$postgres_config")" -eq 0 && \
		docker compose -f deploy/compose/postgres-s3/compose.yaml -f deploy/compose/postgres-s3/compose.dsn-file.yaml -f "$$secret_override" config --quiet && \
		POSTGRES_PASSWORD=validation-only MARKETPLACE_POSTGRES_DSN=postgres://marketplace:validation-only@postgres:5432/marketplace?sslmode=disable AWS_ACCESS_KEY_ID=validation-only AWS_SECRET_ACCESS_KEY=validation-only docker compose -f deploy/compose/postgres-s3/compose.yaml -f deploy/compose/postgres-s3/compose.development.yaml config --quiet; \
	fi

marketplace-gate:
	@set -e; \
		$(MAKE) secrets-check container-check compose-config; \
		$(MAKE) test; \
		E2E_SKIP_DOCKER=1 $(MAKE) e2e; \
		command -v pnpm >/dev/null 2>&1 || { echo "ERROR: pnpm is required for marketplace-gate"; exit 1; }; \
		cd web; \
		pnpm run check; \
		pnpm run test:e2e

# Cross-compilation targets
darwin-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(DIST_DIR)/$(RELEASE_BINARY)-darwin-arm64 $(CMD)

darwin-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(DIST_DIR)/$(RELEASE_BINARY)-darwin-amd64 $(CMD)

linux-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(DIST_DIR)/$(RELEASE_BINARY)-linux-amd64 $(CMD)

linux-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(DIST_DIR)/$(RELEASE_BINARY)-linux-arm64 $(CMD)

release: $(PLATFORMS)

# Release build that injects the community telemetry endpoint and retention
# policy URL at link time. Set AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT
# and AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL to publish values;
# leaving them empty produces a build with the same defaults as `make build`.
RELEASE_TELEMETRY_ENDPOINT ?= $(AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT)
RELEASE_TELEMETRY_RETENTION_URL ?= $(AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL)
RELEASE_EXTRA_LDFLAGS := -X main.version=$(VERSION) \
	-X github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectEndpoint=$(RELEASE_TELEMETRY_ENDPOINT) \
	-X github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectRetentionURL=$(RELEASE_TELEMETRY_RETENTION_URL)

build-release:
	CGO_ENABLED=0 $(GO) build -ldflags "$(RELEASE_EXTRA_LDFLAGS)" -o $(BINARY) $(CMD)

clean:
	rm -rf $(DIST_DIR) $(BINARY)

# Devin runtime probe suite (internal/devinprobe).
#
# Local-only and opt-in. `go test ./...` and CI never run these: without
# AGENT_MARKETPLACE_DEVIN_PROBES=1 every probe skips, so the build never depends
# on a third-party binary being installed.
#
# Every probe runs against an isolated temporary HOME and a throwaway git repo
# and refuses to execute if it can see the real ~/.config/devin; see the package
# doc for the enforced isolation invariants.
#
# The suite is a RELEASE GATE. Acceptance criterion 15 of
# docs/plans/2026-08-03-devin-first-class-harness.md makes symlink probe failure
# release-blocking, so this target is deliberately verbose (-v) and deliberately
# not silenced: a failing symlink probe prints "RELEASE BLOCKER" and exits
# non-zero, and unrun probes are printed on every run so a green suite is never
# read as full coverage.
#
# DEVIN_PROBE_REQUIRE=1 additionally turns a SKIP into a failure. Use it in a
# release check, where "Devin was not installed" must not look like a pass.
devin-probe:
	@echo "Running local Devin runtime probes (isolated HOME; read-only commands) ..."
	@AGENT_MARKETPLACE_DEVIN_PROBES=1 DEVIN_PROBE_REQUIRE=$(DEVIN_PROBE_REQUIRE) \
		$(GO) test -v -count=1 -timeout 10m ./internal/devinprobe/

.PHONY: harness-compat-check harness-compat-config

harness-compat-check:
	$(GO) test ./tools/harness-compat
	HARNESS_COMPAT_TARGET= $(GO) -C e2e test -count=1 -run '^TestCompatibility' .
	shellcheck tools/harness-compat/*.sh

harness-compat-config:
	HARNESS_COMPAT_TARGET=all HARNESS_COMPAT_NATIVE_MODE=unavailable \
		HARNESS_COMPAT_NATIVE_REASON="Configuration-only verification; native probes run in CI" \
		$(GO) -C e2e test -count=1 -timeout 15m -run '^TestHarnessCompatibility$$' .

TUI_QA_PNPM_VERSION := 11.20.0
TUI_QA_BINARY := $(CURDIR)/.local/agent-marketplace/bin/agent-marketplace-tui-qa

tui-qa-check:
	@command -v pnpm >/dev/null || { echo "ERROR: pnpm $(TUI_QA_PNPM_VERSION) is required for TUI QA" >&2; exit 1; }
	@actual="$$(pnpm --version)"; [ "$$actual" = "$(TUI_QA_PNPM_VERSION)" ] || { echo "ERROR: pnpm $(TUI_QA_PNPM_VERSION) is required for TUI QA; found $$actual" >&2; exit 1; }
	cd tools/tui-qa && pnpm check

tui-qa: tui-qa-check
	@mkdir -p "$(dir $(TUI_QA_BINARY))"
	CGO_ENABLED=0 $(GO) build -tags e2e $(LDFLAGS) -o "$(TUI_QA_BINARY)" $(CMD)
	cd tools/tui-qa && AM_TUI_QA_REPO_ROOT="$(CURDIR)" AM_TUI_QA_BINARY="$(TUI_QA_BINARY)" pnpm qa

# Website.
#
# The site is the landing page only. Its harness table and TUI frame
# (site/.vitepress/data/) are checked-in fixtures: no target in this build
# regenerates or verifies them, so they are kept in step with
# pkg/harness-compilation/target by hand. Generation, and the drift check that
# makes it trustworthy, land with the documentation work.
SITE_DIR := site

# Requires Node (see $(SITE_DIR)/package.json); no Go toolchain needed.
site-dev:
	@command -v pnpm >/dev/null || { echo "ERROR: pnpm is required for website development" >&2; exit 1; }
	cd $(SITE_DIR) && pnpm install && pnpm run dev

# SITE_BASE selects the deployment base path, e.g. SITE_BASE=/agent-marketplace/
# for a GitHub Pages project site. Defaults to "/".
site-build:
	@command -v pnpm >/dev/null || { echo "ERROR: pnpm is required for website builds" >&2; exit 1; }
	cd $(SITE_DIR) && pnpm install --frozen-lockfile && pnpm run build

# E2E test suite.
# Runs inside a Docker container for local isolation (doesn't touch host env).
# E2E_PARALLEL: max concurrent sandboxes (default 4, set to 1 for serial).
# E2E_REPORT:   path to JSON report file (default e2e/e2e-report.json).
# E2E_SKIP_DOCKER: set to "1" to run directly on the host (for CI).
# E2E_BASE_IMAGE: override the Docker base image if your environment cannot
# pull the public default image directly.
E2E_IMAGE := agent-marketplace-e2e-runner
E2E_BASE_IMAGE ?=
E2E_BUILD_ARGS :=
ifneq ($(E2E_BASE_IMAGE),)
E2E_BUILD_ARGS := --build-arg BASE_IMAGE=$(E2E_BASE_IMAGE)
endif

e2e:
	@if [ "$(E2E_SKIP_DOCKER)" = "1" ]; then \
		echo "Running E2E tests directly on host (E2E_SKIP_DOCKER=1) ..."; \
		cd e2e && CGO_ENABLED=0 go test -v -count=1 -timeout 20m ./...; \
	else \
		echo "Building E2E runner image ..."; \
		docker build -t $(E2E_IMAGE) $(E2E_BUILD_ARGS) -f e2e/Dockerfile.runner .; \
		echo "Running E2E tests inside Docker container ..."; \
		mkdir -p e2e; \
		rm -f e2e/e2e-report.json; \
		install -m 666 /dev/null e2e/e2e-report.json; \
		docker run --rm \
			-e E2E_PARALLEL=$${E2E_PARALLEL:-4} \
			-e E2E_REPORT=/src/e2e/e2e-report.json \
			-v $$(pwd)/e2e/e2e-report.json:/src/e2e/e2e-report.json \
			$(E2E_IMAGE); \
	fi
