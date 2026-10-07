SHELL := /bin/bash
.DEFAULT_GOAL := help

# Built outside any parent go.work: local runs match CI.
export GOWORK := off

GO ?= go
GOLANGCI_LINT ?= golangci-lint
MODULE := $(shell GOWORK=off $(GO) list -m)
COVER_DIR := cover
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(MODULE)/internal/buildinfo.version=$(VERSION) -X $(MODULE)/internal/buildinfo.commit=$(COMMIT) -X $(MODULE)/internal/buildinfo.buildDate=$(BUILD_DATE)
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
UNIT_PKGS = $(shell GOWORK=off $(GO) list ./... | grep -v /test/acceptance)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'

## init: rename the module after creating a repository from the template (make init NAME=<repo>)
init:
	@test -n "$(NAME)" || { echo "usage: make init NAME=<repository name>"; exit 1; }
	@grep -rl --exclude-dir=.git weaveplatform-template . | xargs sed -i.bak 's/weaveplatform-template/$(NAME)/g'
	@find . -name '*.bak' -not -path './.git/*' -delete
	$(GO) mod tidy
	@echo "renamed to github.com/weaveplatform/$(NAME); review README.md and CODEOWNERS"

## fmt: apply the formatters configured in .golangci.yml
fmt:
	$(GOLANGCI_LINT) fmt --config .golangci.yml ./...

## lint: golangci-lint over the whole module
lint:
	$(GOLANGCI_LINT) run --config .golangci.yml --new=false --fix=false ./...

## vet: go vet
vet:
	$(GO) vet ./...

## test: unit tests (race, shuffle); coverage to cover/unit
test:
	@rm -rf $(COVER_DIR)/unit && mkdir -p $(COVER_DIR)/unit
	$(GO) test -race -shuffle=on -count=1 -cover -coverpkg=$(MODULE)/... $(UNIT_PKGS) -args -test.gocoverdir=$(CURDIR)/$(COVER_DIR)/unit

## cover: merge every cover/* directory and enforce .testcoverage.yml (>=95% total, >=95% per package)
cover:
	@dirs=$$(find $(COVER_DIR) -mindepth 1 -maxdepth 1 -type d ! -name '.merged' | paste -sd, -); \
	if [ -z "$$dirs" ]; then echo "no coverage data; run make test first"; exit 1; fi; \
	rm -rf $(COVER_DIR)/.merged && mkdir -p $(COVER_DIR)/.merged && \
	$(GO) tool covdata merge -i=$$dirs -o=$(COVER_DIR)/.merged && \
	$(GO) tool covdata textfmt -i=$(COVER_DIR)/.merged -o=$(COVER_DIR)/coverage.out && \
	$(GO) tool covdata percent -i=$(COVER_DIR)/.merged
	$(GO) tool go-test-coverage --config=.testcoverage.yml

## vuln: govulncheck (version pinned in go.mod's tool block, kept current with everything else)
vuln:
	$(GO) tool govulncheck ./...

## build: cross-compile every cmd/* binary for each release platform into bin/ (CGO disabled)
build:
	@mkdir -p $(BIN_DIR)
	@for d in $$(find cmd -mindepth 1 -maxdepth 1 -type d); do \
		name=$${d#cmd/}; \
		for p in $(PLATFORMS); do \
			os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
			echo "$$name $$os/$$arch"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
				-o $(BIN_DIR)/$$name-$$os-$$arch$$ext ./$$d || exit 1; \
		done; \
	done

## gate: everything CI runs, in order
gate: vet lint test cover vuln build

.PHONY: help init fmt lint vet test cover vuln build gate

## packer-check: validate pinned Linux Packer templates across the release matrix
packer-check:
	bash scripts/check-packer.sh
	bash scripts/check-native-packer.sh

.PHONY: packer-check

## native-plugin: build, sign on macOS, and install the native Packer plugin locally
native-plugin:
	bash scripts/install-native-plugin.sh

## accept-native: real Packer construction from IMAGEWEAVE_NATIVE_VARS (matching native host)
accept-native:
	@test -n "$(IMAGEWEAVE_NATIVE_VARS)" || { echo "Set IMAGEWEAVE_NATIVE_VARS and IMAGEWEAVE_NATIVE_FAMILY"; exit 1; }
	$(GO) test -count=1 -timeout 3h -v ./test/acceptance

.PHONY: native-plugin accept-native
