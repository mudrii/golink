GO ?= go

.PHONY: build
build:
	$(GO) build ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: race
race:
	$(GO) test -race ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: vuln
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: fmt
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: lint
lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not installed; see https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

.PHONY: ci
ci: vet lint test race vuln
