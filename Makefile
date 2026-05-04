.PHONY: docs build test fmt vet

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./... -count=1

docs:
	tfplugindocs generate --provider-name cloudless

fmt:
	go fmt ./...
