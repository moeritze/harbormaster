BIN := harbormaster
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/moeritze/harbormaster/internal/cli.Version=$(VERSION)

.PHONY: build test lint coverage-check smoke install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/harbormaster

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

coverage-check:
	go test -race -coverprofile=coverage.out ./internal/...
	@go tool cover -func=coverage.out | tail -1 | awk '{gsub("%","",$$3); if ($$3+0 < 80) {print "coverage " $$3 "% is below 80%"; exit 1} else print "coverage " $$3 "%"}'

smoke: build
	./scripts/smoke.sh

install: build
	install -d $(HOME)/.local/bin
	install -m 0755 bin/$(BIN) $(HOME)/.local/bin/$(BIN)
	ln -sf $(HOME)/.local/bin/$(BIN) $(HOME)/.local/bin/hm

clean:
	rm -rf bin coverage.out
