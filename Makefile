GO      ?= go
NPM     ?= npm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
BIN     := bin/devboard

.PHONY: all build web web-embed go-build test lint check dev-api dev-web clean

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

## test: Go tests
test:
	$(GO) test ./...

## lint: gofmt, go vet, svelte-check, eslint
lint: web/node_modules
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
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

clean:
	rm -rf bin web/dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
