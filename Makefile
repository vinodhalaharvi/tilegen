# tilegen - S-expression lowering + tree tiling to Go scaffolding
BIN    := bin/tilegen
SPEC   ?= examples/shop/spec.sexp
CONFIG ?= examples/shop/config.sexp
OUT    ?= out/shop

.DEFAULT_GOAL := help
.PHONY: help build install test cover golden vet fmt fmt-check check demo demo-postgres dump tidy clean

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

build: ## Build bin/tilegen
	go build -trimpath -o $(BIN) .

install: ## Install tilegen into GOBIN
	go install .

test: ## Run the test suite
	go test -count=1 ./...

cover: ## Test with coverage summary
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

golden: ## Rewrite golden files after an intended change to the passes
	go test -run Shop -update ./...

vet: ## go vet
	go vet ./...

fmt: ## gofmt -s -w
	gofmt -s -w .

fmt-check: ## Fail if anything is not gofmt'd
	@test -z "$$(gofmt -s -l . | tee /dev/stderr)" || (echo "run: make fmt"; exit 1)

check: fmt-check vet test ## Everything CI runs

demo: build ## Generate the example (memory storage) and prove it compiles
	$(BIN) -config $(CONFIG) -out $(OUT) -dump $(SPEC)
	cd $(OUT) && go mod tidy && go build ./... && go vet ./...

demo-postgres: build ## Postgres variant: runs sqlc, then builds (needs sqlc on PATH)
	$(BIN) -config examples/shop/config.postgres.sexp -out out/shop-pg -dump $(SPEC)
	cd out/shop-pg && sqlc generate && go mod tidy && go build ./... && go vet ./...

dump: build ## Print the S-expression after every pass
	@$(BIN) -config $(CONFIG) -out $(OUT) -dump $(SPEC) 2>/dev/null
	@for f in $(OUT)/.tilegen/*.sexp; do printf '\n===== %s\n' $$f; cat $$f; done

tidy: ## go mod tidy
	go mod tidy

clean: ## Remove build output and generated demos
	rm -rf bin out coverage.out
