.PHONY: all build test coverage vet clean install snapshot demos build-clockify install-clockify build-timetable install-timetable

all: test build

build:
	go build -o bin/burrowtime ./cmd/burrowtime
	go build -o bin/watson ./cmd/watson

test:
	go test ./...

coverage:
	go test -coverprofile=coverage.out ./...
	@awk 'NR > 1 { total += $$2; if ($$3 > 0) covered += $$2 } END { \
		if (total == 0) { print "No coverage statements found"; exit 1 } \
		printf "Total statement coverage: %.2f%% (minimum 90%%)\n", 100 * covered / total; \
		exit (covered * 100 < total * 90) }' coverage.out

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

build-timetable:
	go build -o bin/burrowtime-timetable ./cmd/burrowtime-timetable

install-timetable:
	go install ./cmd/burrowtime-timetable

snapshot:
	goreleaser release --snapshot --clean

demos: build
	vhs docs/vhs/cli.tape
	vhs docs/vhs/flexible-datetime.tape
	vhs docs/vhs/agent-session.tape
	vhs docs/vhs/aggregate-tui.tape
	vhs docs/vhs/session-messages.tape
	vhs docs/vhs/session-messages-tui.tape
	vhs docs/vhs/session-wizard.tape
	vhs docs/vhs/tui.tape
	vhs docs/vhs/session-edit-tui.tape
