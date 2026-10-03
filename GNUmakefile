BINARY = terraform-provider-smtpfast

default: build

# Build the provider binary.
build:
	go build -o $(BINARY)

# Install the provider into the local GOBIN for manual testing.
install:
	go install

# Run unit tests (fast, no network, no credentials).
test:
	go test ./... -count=1 -timeout 120s

# Run acceptance tests. These create and destroy real resources against the
# SMTPfast API, so they need TF_ACC=1 and a valid SMTPFAST_API_KEY. Use a
# dedicated test account, not production.
testacc:
	TF_ACC=1 go test ./internal/provider/ -v -count=1 -timeout 30m

# Format, vet, and lint.
fmt:
	gofmt -s -w .

vet:
	go vet ./...

lint:
	golangci-lint run

# Regenerate the docs/ folder from schema descriptions and examples.
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name smtpfast

# List API operations that spec-coverage.json does not classify. Fetches the
# live OpenAPI spec, so it needs network access.
spec-coverage:
	go run ./tools/speccoverage

.PHONY: default build install test testacc fmt vet lint docs spec-coverage
