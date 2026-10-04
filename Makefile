# Two products live in this repository (see docs/PRODUCTS.md):
#
#   werkbord        the individual product       cmd/devboard        make build   (the default product)
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
BIN      := bin/devboard
TEAM_BIN := bin/werkbord-team

.PHONY: all build werkbord web web-embed go-build build-team werkbord-team install-team \
        test test-werkbord test-team lint check verify-isolation \
        dev-api dev-web dev-team clean tag verify-tag dist test-install test-install-team

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
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/devboard

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

## test: Go tests (both products and the shared packages) and the individual product's web unit tests
test: web/node_modules
	$(GO) test ./...
	cd web && $(NPM) test

## test-werkbord / test-team: one product's tests (shared packages are tested with both)
test-werkbord: web/node_modules
	$(GO) test $$($(GO) list ./... | grep -v -e /internal/team -e /cmd/werkbord-team)
	cd web && $(NPM) test
test-team:
	$(GO) test ./internal/team/... ./cmd/werkbord-team/...

## lint: gofmt, go vet, svelte-check, eslint
lint: web/node_modules
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@for f in scripts/*.sh; do sh -n $$f || exit 1; done
	$(GO) vet ./...
	cd web && $(NPM) run check && $(NPM) run lint

## check: everything CI should run
check: test lint
	$(GO) build ./...
	cd web && $(NPM) run build

## verify-isolation: build and test the individual product in a copy of the repository with every Team file removed
verify-isolation:
	scripts/verify-isolation.sh

## dev-api / dev-web: run the controller and the Vite dev server (two terminals)
dev-api:
	$(GO) run ./cmd/devboard serve --log-level debug

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

clean:
	rm -rf bin web/dist dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
