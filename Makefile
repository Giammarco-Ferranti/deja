.PHONY: build test test-cover vet install clean

build:
	go build ./cmd/deja

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

install:
	go install ./cmd/deja

clean:
	rm -f deja
