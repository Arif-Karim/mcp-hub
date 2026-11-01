VERSION ?= dev
LDFLAGS := -s -w -X github.com/Arif-Karim/mcp-hub/internal/version.Version=$(VERSION)

.PHONY: build web run verify mutation clean

build: web ## Build UI and embed it into bin/mcp-hub
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mcp-hub ./cmd/mcp-hub

web:
	@if [ -f web/package.json ]; then cd web && npm ci && npm run build; fi
	@touch internal/webui/dist/.gitkeep

run: build
	./bin/mcp-hub serve

verify:
	scripts/verify.sh

mutation:
	scripts/verify.sh --mutation

clean:
	rm -rf bin coverage.out
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
