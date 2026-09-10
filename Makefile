.PHONY: test-acc check lint fmt-check docs-check docs build test fmt vet openapi-refresh

GOLANGCI_LINT_VERSION ?= v2.7.1

# Where the vendored public OpenAPI spec is fetched from. The running API
# serves its own spec (swagger-ui source). The contract the provider ships
# against is production's, so mainnet is the pin; point OPENAPI_URL at stage
# only to look at something that has not reached production yet.
OPENAPI_URL ?= https://api.fluence.dev/docs/fluence-public.yaml

# The single quality gate. CI calls exactly this target; never assemble the
# chain by hand.
check: fmt-check lint vet build test docs-check

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "Files need gofmt:"; echo "$$out"; exit 1; fi

# Regenerates docs/ and fails if the result differs from what is committed.
docs-check: docs
	@git add -N -- docs/ >/dev/null 2>&1 || true
	@git diff --exit-code -- docs/ || { echo "docs/ is stale — run 'make docs' and commit the result"; exit 1; }

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./... -count=1 -race

# Live acceptance tests against the endpoint in FLUENCE_ENDPOINT (stage by
# default via .env); needs FLUENCE_API_KEY. Never part of `check`.
test-acc:
	TF_ACC=1 go test ./internal/provider/ -run 'TestAcc' -count=1 -timeout 30m

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name cloudless

fmt:
	go fmt ./...

# Refresh the vendored public OpenAPI spec from a running vodopad. The
# endpoint returns JSON despite the .yaml path; normalize to YAML so diffs
# stay readable. Override the source with OPENAPI_URL=... .
openapi-refresh:
	curl -fsSL "$(OPENAPI_URL)" -o internal/client/mock/testdata/fluence-public.json
	python3 -c "import json,yaml;d=json.load(open('internal/client/mock/testdata/fluence-public.json'));yaml.safe_dump(d,open('internal/client/mock/testdata/fluence-public.yaml','w'),sort_keys=False,allow_unicode=True)"
	rm -f internal/client/mock/testdata/fluence-public.json
	@echo "refreshed internal/client/mock/testdata/fluence-public.yaml from $(OPENAPI_URL)"
