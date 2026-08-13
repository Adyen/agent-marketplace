VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY := agent-marketplace
RELEASE_BINARY := $(BINARY)-$(VERSION)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
GO := go
CMD := ./cmd/agent-marketplace

# Platform targets
PLATFORMS := darwin-arm64 darwin-amd64 linux-amd64
DIST_DIR := dist

.PHONY: build test lint ci-lint lint-common version release clean e2e devin-probe $(PLATFORMS)
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
	GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_COUNT=0 $(GO) test ./...

lint: LINT_MODE=fix
lint: lint-common

ci-lint: LINT_MODE=check
ci-lint: GOLANGCI_LINT_ARGS=--new-from-rev $(LINT_BASE)
ci-lint: lint-common

lint-common:
	@if [ "$(LINT_MODE)" = "fix" ]; then \
		$(GO) mod tidy; \
		$(GO) fmt ./...; \
	else \
		$(GO) mod tidy -diff; \
		test -z "$$(gofmt -l ./cmd ./internal)" || { echo "ERROR: gofmt found formatting issues - please run 'make lint' and commit the results"; gofmt -d ./cmd ./internal; exit 1; }; \
	fi
	$(GO) vet ./...
	$(run_golangci_lint)

version:
	@echo $(VERSION)

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

release: $(PLATFORMS)

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
