.PHONY: help
help:  ## Show this help menu
	@echo "Usage: make [TARGET ...]"
	@echo ""
	@grep --no-filename -E '^[a-zA-Z_%-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "% -25s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the Go application
	go build -o antibot main.go

.PHONY: run
run: ## Run the Go application
	go run main.go

.PHONY: test
test: ## Run the Go tests
	go test -v ./...

.PHONY: tidy
tidy: ## Tidy the Go module dependencies
	go mod tidy

.PHONY: fmt
fmt: ## Format Go source files
	go fmt ./...
