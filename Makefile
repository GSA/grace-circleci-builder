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

dependencies: $(GOLANGCILINT) go.mod

$(GOLANGCILINT):
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint
