.PHONY: all build test vet lint race clean cross-compile ship-check

GO := go
GOFMT := gofmt
MODULE := github.com/smartass08/aria2go

all: vet test build

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

lint:
	@test -z "$$(gofmt -l . 2>/dev/null)" || (echo "Files not gofmt'd:" && gofmt -l . && exit 1)

bench:
	$(GO) test -bench=. -benchmem ./...

# benchmark — full aria2c vs aria2go comparison
#    make benchmark                  run all scenarios (30 min timeout)
#    make benchmark SHORT=1          skip binary benchmarks (go test -short)
#    make benchmark BENCH_SIZE=1G    override HTTP payload size
benchmark:
	$(GO) test -v -count=1 -timeout=30m ./test/benchmark/

benchmark-short:
	$(GO) test -v -count=1 -short ./test/benchmark/

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

fuzz-short:
	@for dir in $$($(GO) list -f '{{.Dir}}' ./internal/... 2>/dev/null); do \
		if ls $$dir/fuzz_test.go >/dev/null 2>&1; then \
			pkg=$$($(GO) list -f '{{.ImportPath}}' ./$$(echo $$dir | sed 's|.*/aria2go/||')); \
			for fuzz in $$(grep -oP 'func Fuzz\w+' $$dir/fuzz_test.go | sed 's/func //'); do \
				$(GO) test -run='^$$' -fuzz="^$$fuzz$$" -fuzztime=30s $$pkg; \
			done; \
		fi; \
	done

cross-compile:
	GOOS=linux   GOARCH=amd64 $(GO) build -o build/aria2go-linux-amd64   ./cmd/aria2go/
	GOOS=linux   GOARCH=arm64 $(GO) build -o build/aria2go-linux-arm64   ./cmd/aria2go/
	GOOS=darwin  GOARCH=amd64 $(GO) build -o build/aria2go-darwin-amd64  ./cmd/aria2go/
	GOOS=darwin  GOARCH=arm64 $(GO) build -o build/aria2go-darwin-arm64  ./cmd/aria2go/
	GOOS=windows GOARCH=amd64 $(GO) build -o build/aria2go-windows-amd64.exe ./cmd/aria2go/

ship-check: vet test race lint
	@echo "=== ship-check: all gates green ==="

clean:
	rm -rf build/ dist/ coverage.out coverage.html *.prof

fmt:
	$(GOFMT) -w .

tidy:
	$(GO) mod tidy

verify:
	$(GO) mod verify
