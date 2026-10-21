GO ?= go
SWAG ?= swag

.PHONY: build test docs tidy

build:
	$(GO) build ./...

# Package tests share one MySQL/Qdrant test instance, so run them serially.
test:
	$(GO) test -p 1 ./...

# Regenerate the OpenAPI document from handler annotations. Requires the swag
# CLI: go install github.com/swaggo/swag/cmd/swag@latest
docs:
	$(SWAG) init -g cmd/api/main.go -o docs --parseInternal

tidy:
	$(GO) mod tidy
