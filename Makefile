.PHONY: build build-debug test test-cover vet install clean

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
