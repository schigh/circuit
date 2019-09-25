.DEFAULT_GOAL := help

_YELLOW=\033[0;33m
_NC=\033[0m

PKG_DIRS = $(shell go list -f '{{.Dir}}' ./...)

.PHONY: help setup # generic commands
help: ## prints this help
	@grep -hE '^[\.a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "${_YELLOW}%-16s${_NC} %s\n", $$1, $$2}'

setup: ## downloads dependencies
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint
	go get -u github.com/robertkrimen/godocdown/godocdown
	# removes the above packages from go.mod...we only need them locally
	go mod tidy

.PHONY: doc doc-check lint test test-race fmt imports # go commands
doc: ## generates markdown documentation
	@printf "${_YELLOW}generatic godoc documentation...${_NC}\n"
	@for d in ${PKG_DIRS}; do \
		godocdown -o $$d/README.md $$d; \
	done; \

doc-check: ## checks the validity of generated documentation
	@for d in ${PKG_DIRS}; do \
		if [[ ! -f "$$d/README.md" ]]; then echo "file $$d/README.md does not exist"; exit 1; fi; \
		godocdown -o $$d/DOC_CHECK.md $$d; \
		echo "checking $$d/README.md"; \
		cmp -s $$d/README.md $$d/DOC_CHECK.md || exit 1; \
	done; \

lint: ## runs the code linter
	golangci-lint run

test: ## runs unit tests
	go test -cover -v ./...

test-race: ## runs unit tests with race detector
	# this can only run in non-containerized environments
	go test -cover -v -race ./...

fmt: ## apply the gofmt tool
	@gofmt -s -w .

imports: ## apply imports
	@goimports -w -local github.com/schigh/circuit .

prepare: doc fmt imports ## prepare code and documentation for commit
