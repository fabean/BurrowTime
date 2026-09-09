.PHONY: all build test vet clean install snapshot demos build-clockify install-clockify

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

build-clockify:
	go build -o bin/burrowtime-clockify ./cmd/burrowtime-clockify

install-clockify:
	go install ./cmd/burrowtime-clockify

snapshot:
	goreleaser release --snapshot --clean

demos: build
	vhs docs/vhs/cli.tape
	vhs docs/vhs/agent-session.tape
	vhs docs/vhs/tui.tape
