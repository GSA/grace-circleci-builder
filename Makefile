GOBIN := $(GOPATH)/bin
GOLANGCILINT := $(GOBIN)/golangci-lint

.PHONY: default test lint dependencies
default: test

test: lint
	go test -v -cover ./...

lint: dependencies
	$(GOLANGCILINT) run ./...

go.mod:
	go mod init

dependencies: precommit $(GOLANGCILINT) go.mod

$(GOLANGCILINT):
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

precommit:
ifneq ($(strip $(hooksPath)),.github/hooks)
	@git config --add core.hooksPath .github/hooks
endif
