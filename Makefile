BINARY      := calendarctl
BRIDGE      := EventKitBridge
PREFIX      ?= /usr/local
BINDIR      ?= $(PREFIX)/bin
BUILD_DIR   := bin
DIST_DIR    := dist
GO          ?= go
SWIFTC      ?= swiftc
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X github.com/thebuildlabs/calendarctl/internal/cli.version=$(VERSION)
# Developer ID builds also pin the helper's Team ID into the CLI so it rejects
# ad-hoc impostors even if they forge the stable signing identifier.
ifneq ($(SIGN_IDENTITY),)
TEAM_ID     ?= $(shell printf '%s' '$(SIGN_IDENTITY)' | sed -n 's/.*(\([A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9]\)).*/\1/p')
LDFLAGS     += -X github.com/thebuildlabs/calendarctl/internal/bridge.expectedTeamID=$(TEAM_ID)
endif
PLIST       := native/apple/Info.plist
PLIST_BUILD := $(BUILD_DIR)/Info.plist
SWIFT_SRC   := native/apple/EventKitBridge.swift
APP_BUNDLE  := $(BUILD_DIR)/EventKitBridge.app
GO_SOURCES  := $(shell find cmd internal -name '*.go')

# The helper pins its caller's code identity. Local builds are ad-hoc signed
# with stable identifiers; release builds should set SIGN_IDENTITY to a
# Developer ID Application certificate, which also pins the Team ID in the
# generated Info.plist.
SIGN_IDENTITY ?=
CLI_ID      := com.thebuildlabs.calendarctl
BRIDGE_ID   := com.thebuildlabs.calendarctl.bridge

ifeq ($(SIGN_IDENTITY),)
CLI_SIGN    := codesign --force --sign - --identifier $(CLI_ID)
BRIDGE_SIGN := codesign --force --sign - --identifier $(BRIDGE_ID)
BUNDLE_SIGN := codesign --force --sign -
else
CLI_SIGN    := codesign --force --sign "$(SIGN_IDENTITY)" --identifier $(CLI_ID) --options runtime
BRIDGE_SIGN := codesign --force --sign "$(SIGN_IDENTITY)" --identifier $(BRIDGE_ID) --options runtime
BUNDLE_SIGN := codesign --force --sign "$(SIGN_IDENTITY)" --options runtime
endif

.PHONY: all build bridge verify-signing test test-unit test-integration vet vuln fmt check install clean release snapshot run mock docs help

all: build

## build: build the Go CLI and (on macOS) the native EventKit bridge
build: $(BUILD_DIR)/$(BINARY)
ifeq ($(shell uname -s),Darwin)
build: $(APP_BUNDLE)/Contents/MacOS/$(BRIDGE)
endif

$(BUILD_DIR)/$(BINARY): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/calendarctl
	@# The helper pins its caller's code identity, so sign the CLI with a
	@# stable identifier too. Ad-hoc on local builds, Developer ID in release.
	@# Signing failures are fatal: an unsigned CLI cannot satisfy the helper.
	@if [ "$$(uname -s)" = "Darwin" ]; then $(CLI_SIGN) $@; fi

## bridge: compile the native helper and wrap it in a launchable .app bundle
bridge: $(APP_BUNDLE)/Contents/MacOS/$(BRIDGE)

# Generate the embedded/bundle plist from the source template. When a Developer
# ID is used, its Team ID is pinned so the helper rejects ad-hoc "calendarctl"
# impostors. The CLI must be built first to extract that Team ID.
$(PLIST_BUILD): $(PLIST) $(BUILD_DIR)/$(BINARY)
	@mkdir -p $(BUILD_DIR)
	cp $(PLIST) $@
	@if [ -n "$(SIGN_IDENTITY)" ]; then \
		if [ "$$(uname -s)" != "Darwin" ]; then \
			echo "SIGN_IDENTITY requires macOS" >&2; exit 1; \
		fi; \
		team=$$(codesign -dvv $(BUILD_DIR)/$(BINARY) 2>&1 | sed -n 's/^TeamIdentifier=//p' | head -n 1); \
		if [ -z "$$team" ] || [ "$$team" = "not set" ]; then \
			echo "could not read a Developer ID Team ID from $(BUILD_DIR)/$(BINARY); refusing an unpinned helper" >&2; exit 1; \
		fi; \
		plutil -replace CalendarctlTrustedTeamID -string "$$team" $@; \
		echo "pinned CalendarctlTrustedTeamID=$$team"; \
	fi

$(APP_BUNDLE)/Contents/MacOS/$(BRIDGE): $(BUILD_DIR)/$(BRIDGE) $(PLIST_BUILD)
	@mkdir -p $(APP_BUNDLE)/Contents/MacOS
	@# Use a real copy, not APFS clonefile: a clone would let the bundle's
	@# re-sign modify the standalone helper and invalidate its signature.
	install -m 700 $(BUILD_DIR)/$(BRIDGE) $@
	cp $(PLIST_BUILD) $(APP_BUNDLE)/Contents/Info.plist
	@# Sign the inner binary before the bundle; --deep would re-seal the outer
	@# bundle and invalidate the standalone helper's signature.
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		$(BRIDGE_SIGN) $@; \
		$(BUNDLE_SIGN) $(APP_BUNDLE); \
	fi
	@# Only the owning user may execute the TCC-enabled helper.
	@chmod 700 $@
	@echo "Prepared launchable bundle $(APP_BUNDLE) (needed for macOS permission prompts)"

$(BUILD_DIR)/$(BRIDGE): $(SWIFT_SRC) $(PLIST_BUILD)
	@mkdir -p $(BUILD_DIR)
	$(SWIFTC) -O -parse-as-library $(SWIFT_SRC) -o $@ \
		-Xlinker -sectcreate -Xlinker __INFO -Xlinker __info_plist -Xlinker $(PLIST_BUILD)
	@if [ "$$(uname -s)" = "Darwin" ]; then $(BRIDGE_SIGN) $@; fi
	@chmod 700 $@
	@echo "Built and signed $@"

## verify-signing: fail unless release binaries carry the pinned Developer ID Team ID
verify-signing: bridge
	@sh scripts/verify-signing.sh "$(BUILD_DIR)/$(BINARY)" \
		"$(APP_BUNDLE)/Contents/MacOS/$(BRIDGE)" "$(APP_BUNDLE)/Contents/Info.plist"

## test: run the full test suite
test:
	$(GO) test ./...

test-unit:
	$(GO) test ./internal/...

test-integration:
	$(GO) test ./tests/... -count=1

## vet: run static analysis
vet:
	$(GO) vet ./...

## vuln: scan reachable code and dependencies for known vulnerabilities
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...

## fmt: format all Go sources
fmt:
	$(GO) fmt ./...

## check: formatting + vet + tests
check: fmt vet test

## install: install binaries into $(BINDIR)
install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
ifeq ($(shell uname -s),Darwin)
	install -m 0700 $(BUILD_DIR)/$(BRIDGE) $(DESTDIR)$(BINDIR)/$(BRIDGE)
	rm -rf $(DESTDIR)$(BINDIR)/EventKitBridge.app
	cp -R $(APP_BUNDLE) $(DESTDIR)$(BINDIR)/EventKitBridge.app
	chmod 700 $(DESTDIR)$(BINDIR)/EventKitBridge.app/Contents/MacOS/$(BRIDGE)
endif

## run: build and run the CLI with any arguments (make run ARGS="event list --today")
run: build
	./$(BUILD_DIR)/$(BINARY) $(ARGS)

## mock: run against the in-memory mock provider
mock: build
	./$(BUILD_DIR)/$(BINARY) --provider mock $(ARGS)

## release: build signed release artifacts with GoReleaser
release:
	goreleaser release --clean

## snapshot: build a local release snapshot without publishing
snapshot:
	goreleaser release --snapshot --clean

docs:
	@echo "Docs live in docs/ and README.md"

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

help:
	@grep -E '^## ' Makefile | sed 's/## //'
