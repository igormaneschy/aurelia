# Aurelia — build & operations targets.
# See docs/OPERATIONS.md for the full guide.

BINARY        := $(HOME)/.aurelia/bin/aurelia
TMP_BINARY    := $(BINARY).new
PKG           := ./cmd/aurelia
SERVICE_LABEL := com.aurelia.agent
SERVICE       := gui/$(shell id -u)/$(SERVICE_LABEL)
LOG_DIR       := $(HOME)/.aurelia/logs
STDERR_LOG    := $(LOG_DIR)/aurelia.stderr.log
STDOUT_LOG    := $(LOG_DIR)/aurelia.stdout.log

TUI_BINARY    := $(HOME)/.aurelia/bin/aurelia-tui
TUI_PKG       := ./cmd/aurelia-tui
TUI_TMP       := $(TUI_BINARY).new

.PHONY: help build test race vet lint sec cover check bridge install tui install-tui install-service install-service-macos install-service-linux deploy restart sign stop status logs stdout uninstall-service

help:
	@echo "Common targets:"
	@echo "  make build            Compile the binary to $(BINARY)"
	@echo "  make test             Run go test ./... -short"
	@echo "  make race             Run race tests for async hot paths"
	@echo "  make cover            Run go test with coverage profile"
	@echo "  make vet              Run go vet ./..."
	@echo "  make lint             Run golangci-lint"
	@echo "  make sec              Run gosec and govulncheck"
	@echo "  make check            Run lint, security, tests, and vet"
	@echo "  make bridge           Rebuild the TS bridge bundle"
	@echo "  make tui              Compile the TUI binary to $(TUI_BINARY)"
	@echo ""
	@echo "Service targets:"
	@echo "  make install-service     Auto-detect OS and install service"
	@echo "  make install-service-macos  Install/refresh launchd plist (macOS)"
	@echo "  make install-service-linux   Install/refresh systemd service (Linux)"
	@echo "  make deploy              Build atomically + kick the service"
	@echo "  make restart             Restart the service without rebuilding"
	@echo "  make stop                Stop the service (bootout)"
	@echo "  make status              Show service state"
	@echo "  make logs                Tail stderr log"
	@echo "  make stdout              Tail stdout log"
	@echo "  make uninstall-service   Remove plist and unload"

# --- Build ---

build:
	mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) $(PKG)

# Atomic build: write to .new then rename so a running daemon never sees a
# half-written file. On macOS the running process keeps its mmap of the old
# inode, so this is safe even with the service active.
#
# On macOS, the binary is ad-hoc signed (or self-signed if the Aurelia Dev
# certificate exists) so that TCC permissions survive rebuilds. Without a
# stable code identity, each new binary triggers macOS permission prompts.
install:
	mkdir -p $(dir $(BINARY))
	go build -o $(TMP_BINARY) $(PKG)
	mv $(TMP_BINARY) $(BINARY)
	$(MAKE) sign

# --- TUI Build ---

tui:
	mkdir -p $(dir $(TUI_BINARY))
	go build -o $(TUI_BINARY) $(TUI_PKG)

# Atomic build: same .new → mv pattern as install to avoid half-written files.
install-tui:
	mkdir -p $(dir $(TUI_BINARY))
	go build -o $(TUI_TMP) $(TUI_PKG)
	mv $(TUI_TMP) $(TUI_BINARY)

test:
	go test ./... -short -count=1

race:
	go test -race ./internal/pipeline ./internal/telegram

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install: brew install golangci-lint" >&2; exit 1; }
	golangci-lint run --timeout=3m ./...

sec:
	@command -v gosec >/dev/null 2>&1 || { echo "gosec not found. Install: go install github.com/securego/gosec/v2/cmd/gosec@latest" >&2; exit 1; }
	gosec -include=G401,G402,G404,G501,G502,G503,G504,G505,G506,G507,G601,G602 ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: lint sec test race vet
	@echo "✅ All checks passed"

bridge:
	cd bridge && npm run build
	cp bridge/index.ts internal/bridge/bundle.ts
	cp bridge/bundle.js internal/bridge/bundle.js

# --- Service (launchd) ---

install-service:
ifeq ($(shell uname -s),Darwin)
	./scripts/install-service.sh
else
	./scripts/install-systemd.sh
endif

install-service-macos:
	./scripts/install-service.sh

install-service-linux:
	./scripts/install-systemd.sh

# Atomic deploy: build + swap + kickstart. Use this for every change.
deploy: install install-tui
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl kickstart -k $(SERVICE); \
		echo "deployed: $(BINARY) and $(TUI_BINARY) (service kicked)"; \
	else \
		echo "warning: service not loaded — run 'make install-service' first"; \
		echo "binaries updated at: $(BINARY) and $(TUI_BINARY)"; \
	fi

restart:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl kickstart -k $(SERVICE); \
		echo "service restarted"; \
	else \
		echo "service not loaded — run 'make install-service' first" >&2; \
		exit 1; \
	fi

CERT_NAME := Aurelia Dev
KEYCHAIN_PATH := $(HOME)/Library/Keychains/aurelia-codesign.keychain-db
KEYCHAIN_PASS_FILE := $(HOME)/.aurelia/codesign-pass

# sign — codesign the binary so macOS TCC permissions persist across rebuilds.
# Uses a self-signed "Aurelia Dev" certificate if available, otherwise
# falls back to ad-hoc signing (better than unsigned, but still prompts
# on rebuild since ad-hoc identity changes with content).
# Run scripts/setup-codesign.sh once to create the persistent certificate.
sign:
	@if security find-identity -p codesigning "$(KEYCHAIN_PATH)" 2>/dev/null | grep -q "$(CERT_NAME)"; then \
		pass=$$(cat "$(KEYCHAIN_PASS_FILE)" 2>/dev/null || echo ""); \
		security unlock-keychain -p "$$pass" "$(KEYCHAIN_PATH)" 2>/dev/null || true; \
		echo "signing with self-signed certificate: $(CERT_NAME)"; \
		codesign --force --sign "$(CERT_NAME)" -i com.aurelia.agent --options runtime "$(BINARY)"; \
	elif command -v codesign >/dev/null 2>&1; then \
		echo "signing with ad-hoc (run scripts/setup-codesign.sh for persistent cert)"; \
		codesign --force --sign - -i com.aurelia.agent --options runtime "$(BINARY)"; \
	fi

stop:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl bootout $(SERVICE); \
		echo "service stopped"; \
	else \
		echo "service not loaded"; \
	fi

status:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl print $(SERVICE) | awk '/^\tstate = |^\tpid = |^\tlast exit code = |^\tprogram = |^\tpath = /'; \
	else \
		echo "service not loaded"; \
		exit 1; \
	fi

logs:
	@test -f $(STDERR_LOG) || { echo "log not found: $(STDERR_LOG)" >&2; exit 1; }
	tail -n 50 -f $(STDERR_LOG)

stdout:
	@test -f $(STDOUT_LOG) || { echo "log not found: $(STDOUT_LOG)" >&2; exit 1; }
	tail -n 50 -f $(STDOUT_LOG)

uninstall-service:
	@if launchctl print $(SERVICE) >/dev/null 2>&1; then \
		launchctl bootout $(SERVICE) || true; \
	fi
	rm -f $(HOME)/Library/LaunchAgents/$(SERVICE_LABEL).plist
	@echo "service unloaded and plist removed"
