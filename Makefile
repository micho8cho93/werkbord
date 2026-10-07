# Two products live in this repository (see docs/PRODUCTS.md):
#
#   werkbord        the individual product       cmd/werkbord        make build   (the default product)
#   werkbord-team   the Team product             cmd/werkbord-team   make build-team
#
# Each has its own VERSION file, executable, release archives and release tag
# (werkbord-vX.Y.Z / werkbord-team-vX.Y.Z); scripts/product.sh is where that is
# written down. Targets that act on one product take PRODUCT=, e.g.
#   make dist PRODUCT=werkbord-team
GO      ?= go
NPM     ?= npm
PRODUCT ?= werkbord

# Recursive (=), so a product's version is only worked out by a target that uses it.
WERKBORD_VERSION      = $(shell scripts/product.sh werkbord build-version)
WERKBORD_TEAM_VERSION = $(shell scripts/product.sh werkbord-team build-version)
LDFLAGS      = -X main.version=$(WERKBORD_VERSION)
TEAM_LDFLAGS = -X main.version=$(WERKBORD_TEAM_VERSION)
BIN      := bin/werkbord
TEAM_BIN := bin/werkbord-team

.PHONY: all build werkbord web web-embed go-build build-team werkbord-team install-team nebula test-nebula rqlite test-rqlite \
        test test-werkbord test-team lint check verify-isolation \
        desktop desktop-package desktop-release desktop-preview desktop-dev desktop-test desktop-check test-desktop-sign test-desktop-update test-notarize-desktop test-workflows \
        dev-api dev-web dev-team clean tag verify-tag dist test-install test-install-team test-browser team-desktop team-desktop-package team-desktop-release team-desktop-test team-desktop-check test-team-desktop-browser

all: check build build-team

## build (= werkbord): the individual product, with the PWA embedded
build: web web-embed go-build
werkbord: build

web: web/node_modules
	cd web && $(NPM) run build

web/node_modules: web/package-lock.json
	cd web && $(NPM) ci
	@touch $@

# Copy the built PWA where go:embed can see it. .gitkeep keeps the directory
# (and therefore the embed pattern) valid in a fresh clone.
web-embed:
	rsync -a --delete --exclude .gitkeep web/dist/ internal/webui/dist/

go-build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/werkbord

## build-team (= werkbord-team): the Team product. It needs no Node and no web build.
build-team:
	$(GO) build -trimpath -ldflags "$(TEAM_LDFLAGS)" -o $(TEAM_BIN) ./cmd/werkbord-team
werkbord-team: build-team

## install-team: put the Team executable in $(PREFIX)/bin (default ~/.local), from source
PREFIX ?= $(HOME)/.local
install-team: build-team
	install -d $(PREFIX)/bin
	install -m 755 $(TEAM_BIN) $(PREFIX)/bin/werkbord-team
	@echo "installed $(PREFIX)/bin/werkbord-team ($(WERKBORD_TEAM_VERSION))"

## nebula: fetch the pinned Nebula release into .cache/nebula (checked against internal/team/infra/nebula/manifest.go);
## test-nebula: and run Team's tests that start the real program, which then must not be skipped
nebula:
	scripts/fetch-nebula.sh
test-nebula: nebula
	WERKBORD_SKIP_RQLITE=1 WERKBORD_REQUIRE_NEBULA=1 $(GO) test -timeout 30m ./internal/team/infra/... ./internal/team/server/...

## rqlite: fetch (Linux) or build from the pinned source (macOS) the pinned rqlite release into .cache/rqlite (checked against
## internal/team/infra/rqlite/manifest.go);
## test-rqlite: and run Team's tests that start real database clusters, which then must not be skipped: the supervisor, the
## replicated store (failover, partitions, backups, migration from a single file), the server's storage, and the service's own
## tests on a real cluster. One package at a time: clusters elect leaders by timeout, and a starved machine makes them flap.
rqlite:
	scripts/fetch-rqlite.sh
test-rqlite: rqlite
	WERKBORD_REQUIRE_RQLITE=1 $(GO) test -p 1 -timeout 60m ./internal/team/infra/rqlite/... ./internal/team/store/replicated/... ./internal/team/server/...
	WERKBORD_REQUIRE_RQLITE=1 WERKBORD_TEST_STORE=rqlite $(GO) test -timeout 60m ./internal/team/service/

## test: Go tests (both products and the shared packages) and the individual product's web unit tests
test: web/node_modules
	WERKBORD_SKIP_RQLITE=1 $(GO) test -timeout 30m ./...
	cd web && $(NPM) test

## test-werkbord / test-team: one product's tests (shared packages are tested with both)
test-werkbord: web/node_modules
	$(GO) test -timeout 30m $$($(GO) list ./... | grep -v -e /internal/team -e /cmd/werkbord-team)
	cd web && $(NPM) test
test-team:
	WERKBORD_SKIP_RQLITE=1 $(GO) test -timeout 30m ./internal/team/... ./cmd/werkbord-team/...

## lint: gofmt, go vet, svelte-check, eslint
lint: web/node_modules
	@out=$$(gofmt -l cmd internal desktop scripts/appcast); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@for f in scripts/*.sh scripts/test-support/*.sh; do sh -n $$f || exit 1; done
	$(GO) vet ./...
	cd web && $(NPM) run check && $(NPM) run lint

## check: everything CI should run
check: test lint desktop-test team-desktop-test test-notarize-desktop test-workflows
	$(GO) build ./...
	cd web && $(NPM) run build

# ---- the desktop app (desktop/, docs/DESKTOP.md) ----
# It is a Go module of its own because it needs cgo and the system's web view; nothing above builds it, so make check
# never needs a macOS toolchain. UniformTypeIdentifiers is what Wails's file dialogs link against (the Wails command line adds it).
DESKTOP_CGO_LDFLAGS = -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers
DESKTOP_CGO_CFLAGS  = -mmacosx-version-min=13.0
DESKTOP_DEV_DATA   ?= $(CURDIR)/.desktop-dev/data
DESKTOP_DEV_ADDR   ?= 127.0.0.1:7499

## desktop: Werkbord.app for this Mac, in dist/desktop (ad-hoc signed; CODESIGN_IDENTITY signs it for distribution)
desktop: web web-embed
	scripts/build-desktop.sh

## desktop-package: Werkbord.app and Werkbord_<version>_darwin_<arch>.dmg in dist/desktop
desktop-package: web web-embed
	scripts/build-desktop.sh --package

## desktop-release: the signed, notarized, universal disk image a release publishes, in dist/desktop (needs the Developer ID
## and notary credentials in the environment: docs/DESKTOP_RELEASE.md. It refuses to build anything else.)
desktop-release: web web-embed
	scripts/build-desktop.sh --package --release

## desktop-preview: universal Mac DMG for testing before Developer ID approval (not notarized; no Sparkle)
## Published separately as a prerelease, never as Werkbord.dmg or an appcast update.
desktop-preview: web web-embed
	VERSION=v$$(scripts/product.sh werkbord version) ARCH=universal SPARKLE=0 CODESIGN_IDENTITY=- RELEASE=0 NOTARIZE=0 \
		scripts/build-desktop.sh --package dist/desktop-preview
	cp dist/desktop-preview/Werkbord_$$(scripts/product.sh werkbord version)_darwin_universal.dmg dist/desktop-preview/Werkbord-preview.dmg
	cd dist/desktop-preview && shasum -a 256 Werkbord-preview.dmg > Werkbord-preview.dmg.sha256

## desktop-dev: run the window from source, with a controller built from this tree on its own port and data
## (nothing is installed and no login service is made: delete .desktop-dev to start over)
desktop-dev: build
	@mkdir -p $(DESKTOP_DEV_DATA)
	cd desktop && WERKBORD_DESKTOP_CLI=$(CURDIR)/$(BIN) WERKBORD_DATA_DIR=$(DESKTOP_DEV_DATA) WERKBORD_ADDR=$(DESKTOP_DEV_ADDR) \
		CGO_ENABLED=1 CGO_CFLAGS="$(DESKTOP_CGO_CFLAGS)" CGO_LDFLAGS="$(DESKTOP_CGO_LDFLAGS)" \
		$(GO) run -tags desktop,debug -ldflags "-X main.version=$(WERKBORD_VERSION)" .

## desktop-test: the desktop app's logic, which needs no window system and so runs anywhere
desktop-test:
	cd desktop && $(GO) vet ./internal/... && $(GO) test ./internal/...

## desktop-check: desktop-test, and on a Mac also that the window code builds (it needs the Xcode command line tools)
desktop-check: desktop-test
	@if [ "$$(uname -s)" = Darwin ]; then \
		cd desktop && CGO_ENABLED=1 CGO_CFLAGS="$(DESKTOP_CGO_CFLAGS)" CGO_LDFLAGS="$(DESKTOP_CGO_LDFLAGS)" $(GO) vet -tags desktop,production . && \
		CGO_ENABLED=1 CGO_CFLAGS="$(DESKTOP_CGO_CFLAGS)" CGO_LDFLAGS="$(DESKTOP_CGO_LDFLAGS)" $(GO) vet -tags desktop,production,updatertest . ; \
	else echo "desktop-check: the window code builds on macOS only; skipped"; fi

## test-notarize-desktop: notarization (accepted, rejected, no network, wrong or no credentials) and the check of a published release, against fakes of Apple's tools
test-notarize-desktop:
	scripts/test-notarize-desktop.sh

## test-workflows: the GitHub workflows are YAML, and their secrets, permissions and pins are as designed
test-workflows:
	scripts/test-workflows.sh

## test-desktop-update: the app updating itself, with the real Sparkle, two real builds and a feed on this computer (macOS; builds the app twice, so many minutes)
test-desktop-update:
	scripts/test-desktop-update.sh

## test-desktop-sign: how the app is signed, tested without a Developer ID (macOS; builds the app, so minutes; FAST=1 skips that)
test-desktop-sign:
	scripts/test-desktop-updater-config.sh
	scripts/test-desktop-sign.sh $(if $(FAST),--fast)

## verify-isolation: build and test the individual product in a copy of the repository with every Team file removed
verify-isolation:
	scripts/verify-isolation.sh

## dev-api / dev-web: run the controller and the Vite dev server (two terminals)
dev-api:
	$(GO) run ./cmd/werkbord serve --log-level debug

dev-web: web/node_modules
	cd web && $(NPM) run dev -- --host 127.0.0.1

## dev-team: run the Team server in the foreground
dev-team:
	$(GO) run ./cmd/werkbord-team serve --log-level debug

## tag: annotated tag for PRODUCT's VERSION on HEAD, e.g. make tag PRODUCT=werkbord-team (see docs/VERSIONING.md)
tag:
	@tag=$$(scripts/product.sh $(PRODUCT) tag) || exit 1; \
	[ -z "$$(git status --porcelain)" ] || { echo "the working tree is not clean: commit first"; exit 1; }; \
	! git rev-parse -q --verify "refs/tags/$$tag" >/dev/null || { echo "$$tag already exists"; exit 1; }; \
	git tag -a "$$tag" -m "$$tag — $$(git log -1 --format=%s)" && echo "tagged $$tag" && scripts/verify-tag.sh $(PRODUCT)

## verify-tag: check that PRODUCT's tag points at HEAD and agrees with its VERSION file
verify-tag:
	scripts/verify-tag.sh $(PRODUCT)

## dist: release archives and checksums for PRODUCT on every platform (what CI publishes)
dist:
	@[ "$(PRODUCT)" != werkbord ] || $(MAKE) web web-embed
	scripts/build-release.sh $(PRODUCT) v$$(scripts/product.sh $(PRODUCT) version) dist

## test-install: run the installer against a release server on this computer (installs nothing outside a temp dir)
test-install: web web-embed
	scripts/test-install.sh

## test-install-team: the same for Team's installer
test-install-team:
	scripts/test-install-team.sh

## test-browser: disposable desktop/mobile browsers plus real-process Team handoff
# Install Chromium once with: cd web && npx playwright install chromium
test-browser: build build-team
	node scripts/test-browser.cjs

clean:
	rm -rf bin web/dist dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete

## team-desktop: separate Team native app, persistent system service and pinned sidecars (macOS)
team-desktop: web web-embed
	scripts/build-team-desktop.sh
team-desktop-package: web web-embed
	scripts/build-team-desktop.sh --package
team-desktop-release: web web-embed
	scripts/build-team-desktop.sh --release
team-desktop-test:
	cd cmd/werkbord-team/desktop && $(GO) test ./internal/platform && $(GO) vet ./internal/platform
team-desktop-check: team-desktop-test
	cd cmd/werkbord-team/desktop && CGO_ENABLED=1 CGO_LDFLAGS="-framework UniformTypeIdentifiers" $(GO) build -tags desktop,production -o /tmp/werkbord-team-window-check .
test-team-desktop-browser: web web-embed rqlite
	node scripts/test-team-desktop-browser.cjs
