.PHONY: build build-debug test test-cover vet install clean sqlc sqlc-verify

# Match .goreleaser.yaml so a source build is the same size as a released one.
# Without these, `go build` retains the symbol table and DWARF — 12.4 MB against
# the 7.5 MB that actually ships. `-trimpath` additionally keeps local absolute
# paths out of the binary.
RELEASE_FLAGS := -trimpath -ldflags="-s -w"

build:
	go build $(RELEASE_FLAGS) -o bin/deja ./cmd/deja

# Unstripped, DWARF intact, for delve and friends.
build-debug:
	go build -o bin/deja ./cmd/deja

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

install:
	go install $(RELEASE_FLAGS) ./cmd/deja

clean:
	rm -rf bin
	rm -f deja

# sqlc is a build-time tool only — nothing it emits is a runtime dependency.
# Install with: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
sqlc:
	sqlc generate

# Fails if the committed generated code is stale. Worth wiring into CI, since
# the code is checked in and a forgotten `make sqlc` is otherwise invisible.
sqlc-verify:
	sqlc diff
