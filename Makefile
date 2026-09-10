.PHONY: docs build test fmt vet openapi-refresh

# Path to a local vodopad checkout used to regenerate the vendored OpenAPI spec.
VODOPAD_DIR ?= ../vodopad

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

# Regenerate the vendored public OpenAPI spec from a local vodopad checkout.
# Override the path with: make openapi-refresh VODOPAD_DIR=/path/to/vodopad
openapi-refresh:
	cd $(VODOPAD_DIR) && OPEN_API_OUT_DIR=docs cargo run --quiet --bin gen_openapi
	cp $(VODOPAD_DIR)/docs/fluence-public.yaml internal/client/mock/testdata/fluence-public.yaml
	@echo "refreshed internal/client/mock/testdata/fluence-public.yaml from $(VODOPAD_DIR)"
