.DEFAULT_GOAL := help

_YELLOW=\033[0;33m
_NC=\033[0m

PKG_DIRS = $(shell go list -f '{{.Dir}}' ./...)

.PHONY: help setup # generic commands
help: ## prints this help
	@grep -hE '^[\.a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "${_YELLOW}%-16s${_NC} %s\n", $$1, $$2}'

setup: ## downloads dependencies
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint
	# removes the above packages from go.mod...we only need them locally
	go mod tidy

.PHONY: lint test test-race fmt imports # go commands
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

prepare: fmt imports ## prepare code and documentation for commit
