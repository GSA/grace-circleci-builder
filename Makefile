GOBIN := $(GOPATH)/bin
GODEP := $(GOBIN)/dep
GOLANGCILINT := $(GOBIN)/golangci-lint
GOSEC := $(GOBIN)/gosec

.PHONY: default test lint dependencies
default: test

test: lint
	go test -v -cover ./...

lint: dependencies
	$(GODEP) ensure
	$(GOLANGCILINT) run ./...
	$(GOSEC) ./...

Gopkg.toml: $(GODEP)
ifeq (,$(wildcard Gopkg.toml))
	$(GODEP) init
endif

dependencies: $(GOLANGCILINT) $(GOSEC) Gopkg.toml

$(GOLANGCILINT):
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

$(GODEP):
	go get -u github.com/golang/dep/cmd/dep

$(GOSEC):
	go get -u github.com/securego/gosec/cmd/gosec
