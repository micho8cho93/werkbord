GO      ?= go
NPM     ?= npm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
BIN     := bin/devboard

.PHONY: all build web web-embed go-build test lint check dev-api dev-web clean tag dist test-install

all: check build

## build: production binary with the PWA embedded
build: web web-embed go-build

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

## test: Go tests and web unit tests
test: web/node_modules
	$(GO) test ./...
	cd web && $(NPM) test

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

## dev-api / dev-web: run the controller and the Vite dev server (two terminals)
dev-api:
	$(GO) run ./cmd/devboard serve --log-level debug

dev-web: web/node_modules
	cd web && $(NPM) run dev -- --host 127.0.0.1

## tag: annotated version tag on HEAD, e.g. make tag VERSION=v0.7.0 (see docs/VERSIONING.md)
tag:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "VERSION must look like v0.7.0"; exit 1; }
	@[ -z "$$(git status --porcelain)" ] || { echo "the working tree is not clean: commit first"; exit 1; }
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || { echo "$(VERSION) already exists"; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION) — $$(git log -1 --format=%s)"

## dist: release archives and checksums for every platform, e.g. make dist VERSION=v0.7.0 (what CI publishes)
dist: web web-embed
	scripts/build-release.sh $(VERSION) dist

## test-install: run the installer against a release server on this computer (installs nothing outside a temp dir)
test-install: web web-embed
	scripts/test-install.sh

clean:
	rm -rf bin web/dist dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
