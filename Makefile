.PHONY: build test vet fmt fmt-check check run emit-ast emit-schema clean

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go')

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed for:"; echo "$$out"; exit 1; fi

# fmt-check, vet, and test together: what CI would run.
check: fmt-check vet test

# Source/sink paths in a .sift file resolve relative to the script's own
# directory (see cmd/sift/run.go), so this works from the repo root
# without cd'ing into examples/ first.
run: build
	go run ./cmd/sift run examples/adults.sift

emit-ast: build
	go run ./cmd/sift --emit-ast examples/adults.sift

emit-schema: build
	go run ./cmd/sift --emit-schema examples/adults.sift

clean:
	rm -f examples/adults.jsonl examples/*_out.jsonl examples/*_errors.jsonl
