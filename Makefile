.PHONY: all build test vet clean install snapshot

all: test build

build:
	go build -o bin/burrowtime ./cmd/burrowtime
	go build -o bin/watson ./cmd/watson

test:
	go test ./...

vet:
	go vet ./...

clean:
	go clean

install:
	go install ./cmd/burrowtime ./cmd/watson

snapshot:
	goreleaser release --snapshot --clean
