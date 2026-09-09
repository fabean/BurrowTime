# Contributing to BurrowTime

Small, focused pull requests are easiest to review. Open an issue first for a
new command, storage-format change, or compatibility decision so the behavior
can be agreed before implementation.

## Set up the repository

BurrowTime requires Go 1.24 or newer.

```bash
git clone https://github.com/fabean/BurrowTime.git
cd BurrowTime
go mod download
```

## Run the checks

```bash
make test
make vet
go test -race ./...
```

Use a temporary data directory when exercising commands by hand:

```bash
burrowtime_test_dir="$(mktemp -d)"
BURROWTIME_DIR="$burrowtime_test_dir" go run ./cmd/burrowtime
```

Do not point development builds at a real BurrowTime or Watson history.

## Work on the site

The Astro site is maintained in the separate `burrowtime-site` repository.
See that repository's README for its development and deployment commands.

## Update terminal demos

The checked-in GIFs are generated from reproducible VHS tapes. Install VHS
0.11.0 plus its `ttyd` and `ffmpeg` dependencies, then run:

```bash
make demos
```

Each tape uses isolated directories under `/tmp`. Inspect every rendered GIF
before committing it.

## Compatibility work

BurrowTime targets Watson 2.1.0 behavior for its shared command surface and
core files. Tests in `internal/cli/oracle_test.go` compare the Go program with
Python Watson as a black box. Keep BurrowTime-specific metadata out of the four
compatible files: `config`, `frames`, `state`, and `last_sync`.

## Pull requests

- Add tests for behavior changes.
- Update the README and web docs when a user-facing command changes.
- Do not commit `bin/` or `dist/`.
- Keep release tags for reviewed versions. The release workflow publishes any
  tag that matches `v*`.

By contributing, you agree that your work is licensed under the repository's
MIT License.
