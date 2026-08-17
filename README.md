# BurrowTime

<p align="center">
  <img src="assets/burrowtime-mascot.png" alt="BurrowTime gopher mascot holding a pocket watch beside a terminal in its burrow" width="360">
</p>

**Fast, local time tracking with a friendly terminal UI and Watson-compatible
commands and data.**

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/josh/burrowtime?display_name=tag&sort=semver)](https://github.com/josh/burrowtime/releases/latest)

BurrowTime is a Go port of [Watson](https://github.com/jazzband/Watson), the
command-line time tracker. It adds an interactive Bubble Tea dashboard while
preserving Watson's commands and on-disk format. Existing Watson users can
copy their history into BurrowTime on first launch and move it back later—no
conversion, account, or hosted service is required.

```console
$ burrowtime start "client portal" +review
Starting project client portal [review] at 09:42

$ burrowtime stop
Stopping project client portal [review], started 38 minutes ago and stopped just now. (id: 82fd91a)

$ burrowtime report --week
Mon 17 August 2026 -> Mon 17 August 2026

client portal - 2h 14m 08s
```

## Why BurrowTime?

- **A real terminal UI.** Run `burrowtime` by itself for a live dashboard,
  recent activity, interactive logs, report charts, date ranges, and filtering.
- **A scriptable CLI.** Direct commands, JSON, CSV, pipes, shell completions,
  and stable exit codes remain first-class features.
- **Watson-compatible data.** BurrowTime uses Watson's `config`, `frames`,
  `state`, and `last_sync` formats in its own safe data directory.
- **Local by default.** Your time data stays in ordinary files on your machine.
- **One small Go program.** No Python environment or database is required.

## BurrowTime compared with Watson

BurrowTime preserves Watson's familiar workflow, but it is not merely a renamed
Watson executable:

| Area | Watson 2.1.0 | BurrowTime |
| --- | --- | --- |
| Runtime | Python application | Standalone Go binaries |
| Running with no command | Non-interactive CLI behavior | Bubble Tea dashboard |
| CLI commands and output | Original behavior | Watson-compatible behavior, JSON, CSV, and exit codes |
| Data location | Watson directory and `WATSON_DIR` | Separate BurrowTime directory and `BURROWTIME_DIR` |
| Data format | `config`, `frames`, `state`, `last_sync` | Same formats, plus `active_timers` when concurrency is used |
| Active timers | One | One by default, with explicit concurrent timers available |
| Stopping multiple timers | Not applicable | Interactive picker, `--timer`, or `--all` |
| Logs and reports | CLI output and filters | Compatible CLI plus interactive views, charts, ranges, and filters |
| Moving data | Operates directly on its own directory | Copies data from or back to Watson with confirmation and backups |
| Safety differences | Original behavior | Guards two known crash/data-corruption edge cases described below |

The release archive also includes a `watson` compatibility executable. Unlike
the primary `burrowtime` command, that executable keeps Watson's data-directory
and non-interactive bare-command behavior for users who want a direct Go-based
replacement.

## Installation

### Go install

With Go 1.24 or newer:

```bash
go install github.com/josh/burrowtime/cmd/burrowtime@latest
```

The optional drop-in `watson` executable can be installed alongside it:

```bash
go install github.com/josh/burrowtime/cmd/watson@latest
```

### Release archives

Tagged releases provide checksummed archives for Linux, macOS, and Windows on
the [GitHub Releases page](https://github.com/josh/burrowtime/releases). Each
archive contains both `burrowtime` and the optional `watson` compatibility
binary.

Package-manager recipes will be advertised here only after their first release
has been published and tested. Maintainers can follow the
[distribution guide](docs/DISTRIBUTION.md) for AUR, Homebrew, and Scoop.

### Build from source

```bash
git clone https://github.com/josh/burrowtime.git
cd burrowtime
make build
./bin/burrowtime
```

## Getting started

Start and stop a timer from the CLI:

```bash
burrowtime start "deep work" +planning
burrowtime status
burrowtime stop
```

Projects may contain spaces. Tags begin with `+`; both are carried into logs
and reports.

```bash
burrowtime start "acme redesign" +frontend +meeting
```

Run BurrowTime without a subcommand to open the dashboard:

```bash
burrowtime
```

On the first interactive use, BurrowTime checks whether its default data
directory is empty. If it finds an existing Watson installation, it offers to
copy that data into BurrowTime; pressing Enter accepts. Watson's original files
are never moved or changed. Declining is remembered, and scripts are never
blocked waiting for this prompt. You can still import manually later.

The dashboard and direct CLI use the same storage and reporting code. Actions
started in the TUI are handed to the normal command implementation, so there
is no separate or reduced interactive mode.

### Concurrent timers

With the default settings, starting another project remains safe: if any timer
is active, an ordinary `start` asks you to resolve it first. Choose the behavior
explicitly when you need something else:

```bash
# Stop every active timer, then start this one.
burrowtime start --stop "client call" +meeting

# Keep existing timers running and add another.
burrowtime start --concurrent "background export" +ops
```

If Watson's `options.stop_on_start` setting is enabled, `--no-stop` restores
the refuse-to-replace behavior for one invocation. `--stop` and `--concurrent`
cannot be combined, and a concurrent timer cannot use `--no-gap`.

`burrowtime status` lists every active timer and its short ID. With multiple
timers, an interactive `burrowtime stop` opens a picker. Scripts must be
explicit:

```bash
burrowtime stop --timer 4e169a2
burrowtime stop --all
```

The Watson-compatible `state` file continues to hold one primary timer.
Additional timers are stored in BurrowTime's `active_timers` companion file;
when the primary is stopped, another active timer is promoted automatically.
Every completed timer is written as an ordinary Watson frame. Reports count
each timer's full duration, so overlapping time contributes independently to
each project and to the combined total.

### Command overview

| Command | Purpose |
| --- | --- |
| `burrowtime` | Open the interactive dashboard |
| `start`, `stop`, `status` | Control and inspect live timers |
| `restart`, `cancel` | Resume a frame or discard a running timer |
| `add`, `edit`, `remove` | Maintain historical frames |
| `log` | List individual sessions |
| `report` | Summarize time by project |
| `aggregate` | Aggregate project time by day |
| `projects`, `tags`, `frames` | List known values and frame IDs |
| `rename`, `merge` | Rewrite names or merge a frames file |
| `config` | Read or update Watson-compatible settings |
| `sync` | Synchronize with a Watson-compatible server |
| `migrate` | Copy data between BurrowTime and Watson |
| `completion` | Generate shell completion scripts |

### TUI controls

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Move or scroll |
| `enter` | Open the selected action |
| `l` / `r` | Open Log or Report |
| `s` / `x` | Start or stop a timer |
| `←` / `→` | Change the report range |
| `/` | Filter by project, tag, frame ID, or date |
| `tab` | Switch between Log and Report |
| `b` / `esc` | Go back |
| `q` | Quit |

Mouse-wheel scrolling also works in the Log and Report views.

## Logs, reports, and filters

Use a shortcut range or explicit dates:

```bash
burrowtime log --day
burrowtime log --month --project "client portal"
burrowtime report --week --tag billable
burrowtime report --from 2026-08-01 --to 2026-08-15
burrowtime aggregate --year
```

Use JSON or CSV when another program will consume the result:

```bash
burrowtime log --all --json | jq '.[0]'
burrowtime report --month --csv > august.csv
```

Run `burrowtime <command> --help` for every available flag.

## Watson compatibility and migration

BurrowTime targets Watson 2.1.0 behavior and includes the complete command set:

```text
start       stop        restart     cancel      status
add         edit        remove      rename      merge
log         report      aggregate   projects    tags
frames      config      sync        completion
```

The `watson` build reports Watson's compatible version and retains Watson's
non-interactive bare-command behavior. It continues to use Watson's own data
directory and `WATSON_DIR`:

```bash
watson start project +tag
watson log --week
```

BurrowTime intentionally fixes two unsafe Watson edge cases: an empty-history
`start --no-gap` returns an error instead of crashing, and resolving a merge
conflict never persists temporary display markup into a tag.

Compatibility means the four core files retain Watson's encoding and the
standard commands target Watson 2.1.0 behavior. The implementation is tested
against Python Watson as a black-box oracle. BurrowTime-specific features are
kept outside that core format:

- concurrent timers live in the additive `active_timers` file;
- the first running timer remains in the compatible `state` file;
- completed concurrent timers become ordinary Watson frames;
- the Bubble Tea UI calls the same command and reporting implementation as the
  non-interactive CLI.

Import and export are deliberate snapshot copies, not continuous two-way sync.
After importing, new Watson activity does not automatically appear in
BurrowTime, and new BurrowTime activity does not appear in Watson until you
export it. This prevents either program from unexpectedly mutating the other's
working data.

### Moving from Watson

The first-run prompt is the easiest path. You can also import explicitly:

```bash
burrowtime migrate from-watson
```

The command copies `config`, `frames`, `state`, and `last_sync` without changing
the Watson installation. If BurrowTime already contains data, it asks before
replacing it and creates a timestamped backup under `.burrowtime-backups/`.
For scripts, confirmation must be explicit:

```bash
burrowtime migrate from-watson --force
```

### Moving back to Watson

Export the compatible data files back into Watson with:

```bash
burrowtime migrate to-watson
```

Existing Watson data is confirmed and backed up before replacement. Watson can
represent only one active timer, so BurrowTime refuses to export while
additional concurrent timers are running. Stop those timers first, then retry.
The `active_timers` extension itself is never exported to Watson.

For a non-interactive export that intentionally replaces the destination:

```bash
burrowtime migrate to-watson --force
```

Use custom source or destination directories when needed:

```bash
burrowtime --data-dir /path/to/burrowtime \
  migrate from-watson --watson-data-dir /path/to/watson

burrowtime --data-dir /path/to/burrowtime \
  migrate to-watson --watson-data-dir /path/to/watson --force
```

BurrowTime uses `BURROWTIME_DIR` when set. Otherwise its default is normally
`~/.config/burrowtime` on Linux, the platform config directory on Windows, and
`~/Library/Application Support/burrowtime` on macOS. `--data-dir` selects a
directory for one CLI invocation. The `watson` compatibility binary separately
honors `WATSON_DIR` and Watson's platform defaults.

Writes are atomic and retain Watson-style `.bak` files. `TZ` is honored for
date parsing and display.

## Shell completion

BurrowTime generates completion scripts dynamically, including known projects,
tags, and frame IDs.

```bash
# Bash
source <(burrowtime completion bash)

# Zsh
burrowtime completion zsh > "${fpath[1]}/_burrowtime"

# Fish
burrowtime completion fish > ~/.config/fish/completions/burrowtime.fish

# PowerShell
burrowtime completion powershell | Out-String | Invoke-Expression
```

## Development

Run the dashboard directly from the checkout:

```bash
go run ./cmd/burrowtime
```

Arguments after the package path are passed to BurrowTime normally:

```bash
go run ./cmd/burrowtime start "local test" +development
go run ./cmd/burrowtime status
go run ./cmd/burrowtime stop
```

Use a temporary directory when testing storage or migration behavior without
touching your real history:

```bash
burrowtime_test_dir="$(mktemp -d)"
BURROWTIME_DIR="$burrowtime_test_dir" go run ./cmd/burrowtime
```

The single-file spelling also works—`go run cmd/burrowtime/main.go`—but the
package form above is the conventional choice.

Run the project checks with:

```bash
make build
make test
make vet
go test -race ./...
```

The test suite exercises the Go implementation against Watson as a black-box
oracle, including reports, editors, configuration rewrites, Unicode, sync wire
format, and alternating Watson/BurrowTime writes.

To inspect release artifacts without publishing them:

```bash
goreleaser release --snapshot --clean
```

See [PORTING_PLAN.md](PORTING_PLAN.md) for the compatibility strategy and
[docs/DISTRIBUTION.md](docs/DISTRIBUTION.md) for release maintenance.

## License

BurrowTime is distributed under the [MIT License](LICENSE) and derives from
the MIT-licensed Watson project.
