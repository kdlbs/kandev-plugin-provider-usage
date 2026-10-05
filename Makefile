.PHONY: build test test-package-verifier test-release-version fmt check-format vet \
	package package-host package-file verify-package verify-package-host clean

BIN := bin/kandev-provider-usage
VERSION := 0.10.0
STAGE := .build/stage
PKG_OUT := kandev-provider-usage-$(VERSION).tar.gz
VERIFY_FULL := .build/verify-full
VERIFY_HOST := .build/verify-host
TEST_TMP := $(CURDIR)/.build/tmp
NODE ?= node

## plugin-pack lives in the kandev monorepo. Run it from THAT module (go -C) so
## its own dependencies resolve against the SDK's go.sum: running it from here
## would need go.sum entries for packages this plugin never imports, which
## `go mod tidy` (enforced in CI) then strips again.
SDK := ../kandev/apps/backend
PACK := go -C $(SDK) run ./cmd/plugin-pack

## Build the plugin binary for the host platform (development use; the
## installed-plugin path always goes through `make package`/`package-host`).
build:
	mkdir -p bin
	go build -o $(BIN) ./server

test:
	go test ./server/...
	$(NODE) --test test/*.test.mjs
	$(NODE) --check ui/bundle.js
	$(MAKE) test-package-verifier
	$(MAKE) test-release-version

test-package-verifier:
	mkdir -p $(TEST_TMP)
	TMPDIR=$(TEST_TMP) sh scripts/test-verify-package.sh

test-release-version:
	mkdir -p $(TEST_TMP)
	TMPDIR=$(TEST_TMP) sh scripts/test-verify-release-version.sh

fmt:
	gofmt -l .

check-format:
	@test -z "$$(gofmt -l ./server)" || { printf 'gofmt needed:\n'; gofmt -l ./server; exit 1; }

vet:
	go vet ./server/...

## Cross-compile server/plugin-<goos>-<goarch>[.exe] for every platform
## declared in manifest.yaml's runtime.executables, stage manifest.yaml + ui/
## alongside them, and pack the tree with the kandev repo's plugin-pack
## (resolved via this repo's local `replace` directive). Install via
## Settings > Plugins (upload) or
## `curl -F package=@$(PKG_OUT) http://localhost:<port>/api/plugins/install`.
package:
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r ui $(STAGE)/ui
	GOOS=linux   GOARCH=amd64 go build -o $(STAGE)/server/plugin-linux-amd64       ./server
	GOOS=linux   GOARCH=arm64 go build -o $(STAGE)/server/plugin-linux-arm64       ./server
	GOOS=darwin  GOARCH=amd64 go build -o $(STAGE)/server/plugin-darwin-amd64      ./server
	GOOS=darwin  GOARCH=arm64 go build -o $(STAGE)/server/plugin-darwin-arm64      ./server
	GOOS=windows GOARCH=amd64 go build -o $(STAGE)/server/plugin-windows-amd64.exe ./server
	$(PACK) -dir $(abspath $(STAGE)) -out $(abspath $(PKG_OUT))
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Package for the host platform only — faster local iteration than the full
## 5-platform `make package`.
package-host:
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r ui $(STAGE)/ui
	go build -o $(STAGE)/server/plugin-$$(go env GOOS)-$$(go env GOARCH)$$(go env GOEXE) ./server
	$(PACK) -dir $(abspath $(STAGE)) -out $(abspath $(PKG_OUT)) -platform-only
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Verify the complete archive against the manifest, exact file inventory, and checksums.
verify-package: package
	@set -eu; \
		rm -rf "$(VERIFY_FULL)"; \
		mkdir -p "$(VERIFY_FULL)"; \
		tar -xzf "$(PKG_OUT)" -C "$(VERIFY_FULL)"; \
		sh scripts/verify-package.sh "$(VERIFY_FULL)" full

## Verify the host-platform archive using the same checks as full packaging.
verify-package-host: package-host
	@set -eu; \
		rm -rf "$(VERIFY_HOST)"; \
		mkdir -p "$(VERIFY_HOST)"; \
		tar -xzf "$(PKG_OUT)" -C "$(VERIFY_HOST)"; \
		sh scripts/verify-package.sh "$(VERIFY_HOST)" host "$$(go env GOOS)-$$(go env GOARCH)"

## Print the archive name for release checks without building it.
package-file:
	@printf '%s\n' "$(PKG_OUT)"

clean:
	rm -rf bin .build kandev-provider-usage-*.tar.gz
