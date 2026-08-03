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

# Source paths in a .sift file resolve relative to the process's working
# directory (see cmd/sift/run.go), not the script's own directory — so
# this run from testdata/, not the repo root, mirrors the one real
# constraint that choice puts on how adults.sift must be invoked.
run: build
	cd testdata && go run ../cmd/sift run adults.sift

emit-ast: build
	go run ./cmd/sift --emit-ast testdata/adults.sift

emit-schema: build
	go run ./cmd/sift --emit-schema testdata/adults.sift

clean:
	rm -f testdata/adults.jsonl
