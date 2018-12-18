default: test

test: test_go

test_go: validate_go
	go test ./...

validate_go: dep_ensure
	gometalinter --deadline=240s --vendor ./...
	gosec ./...

dep_init:
ifeq (,$(wildcard ./Gopkg.toml))
	dep init
endif

dep_ensure: dep_init
	dep ensure