VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X shipyard/internal/version.Version=$(VERSION)

.PHONY: build web web-dev test clean release

RELEASE_TARGETS := linux/amd64 linux/arm64 linux/arm/7 darwin/arm64 darwin/amd64

build: web
	go build -ldflags "$(LDFLAGS)" -o shipyard .

web:
	cd web && npm ci --no-audit --no-fund && npm run build

# Frontend with hot reload against a running `shipyard ui --port 7878` or `shipyard serve`.
web-dev:
	cd web && npm run dev

test:
	go test ./...
	cd web && npm run typecheck

# Release binaries for every platform in dist/, each with its .sha256,
# built on this machine. Fails if the web UI is missing from any binary.
#   make release VERSION=v0.5.0
#   gh release create v0.5.0 dist/* --title v0.5.0 --generate-notes
release: web
	@case "$(VERSION)" in v[0-9]*) ;; *) echo "set VERSION=vX.Y.Z (got $(VERSION))"; exit 1 ;; esac
	@test -f web/dist/index.html || { echo "web UI missing from web/dist"; exit 1; }
	go test ./...
	mkdir -p dist
	find dist -mindepth 1 -delete
	@set -e; for t in $(RELEASE_TARGETS); do \
	  os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; arm=""; \
	  case "$$rest" in */*) arm=$${rest#*/} ;; esac; \
	  name="shipyard-$$os-$$arch$${arm:+v$$arm}"; \
	  echo "building dist/$$name"; \
	  GOOS=$$os GOARCH=$$arch GOARM=$$arm CGO_ENABLED=0 \
	    go build -ldflags "-s -w -X shipyard/internal/version.Version=$(VERSION)" -o "dist/$$name" . ; \
	  grep -q 'id="root"' "dist/$$name" || { echo "web UI not embedded in dist/$$name"; exit 1; }; \
	  if command -v sha256sum >/dev/null; then (cd dist && sha256sum "$$name" > "$$name.sha256"); \
	  else (cd dist && shasum -a 256 "$$name" > "$$name.sha256"); fi; \
	done
	@ls -lh dist

clean:
	rm -f shipyard
	find web/dist -mindepth 1 ! -name .gitkeep -delete
	if [ -d dist ]; then find dist -mindepth 1 -delete; fi
