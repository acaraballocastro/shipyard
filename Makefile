VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X shipyard/internal/version.Version=$(VERSION)

.PHONY: build web web-dev test clean

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

clean:
	rm -f shipyard
	find web/dist -mindepth 1 ! -name .gitkeep -delete
