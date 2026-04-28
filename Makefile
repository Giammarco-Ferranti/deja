.PHONY: build test vet install clean

build:
	go build ./cmd/deja

test:
	go test ./...

vet:
	go vet ./...

install:
	go install ./cmd/deja

clean:
	rm -f deja
