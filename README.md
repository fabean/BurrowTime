# BurrowTime

<p align="center">
  <img src="assets/burrowtime-mascot.png" alt="BurrowTime gopher mascot holding a pocket watch beside a terminal in its burrow" width="360">
</p>

**Local time tracking for you and your coding agents, with a friendly terminal
UI and Watson-compatible commands and data.**

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/fabean/BurrowTime?display_name=tag&sort=semver)](https://github.com/fabean/BurrowTime/releases/latest)

BurrowTime is a Go port of [Watson](https://github.com/jazzband/Watson), the
command-line time tracker. It adds an interactive Bubble Tea dashboard while
preserving Watson's commands and on-disk format. Existing Watson users can
copy their history into BurrowTime on first launch and move it back later—no
conversion, account, or hosted service is required.

BurrowTime also gives coding agents a safe way to track the work you explicitly
ask them to perform. Agent sessions use the same local timers and reports you
use yourself, with exact ownership, retry protection, leases for interrupted
work, and integrations for popular coding agents and MCP clients.

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
- **Built for coding agents.** Codex, Claude Code, Cursor, Gemini, OpenCode, and
  MCP clients can track only the work you request, without taking control of
  unrelated timers.
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
| Data format | `config`, `frames`, `state`, `last_sync` | Same formats, plus companion files for concurrency and agent sessions |
| Active timers | One | One by default, with explicit concurrent timers available |
| Coding agents | No dedicated workflow | Owned sessions, safe retries, leases, skills, and MCP tools |
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
go install github.com/fabean/BurrowTime/cmd/burrowtime@latest
```

The optional drop-in `watson` executable can be installed alongside it:

```bash
go install github.com/fabean/BurrowTime/cmd/watson@latest
```

### Release archives

Tagged releases provide checksummed archives for Linux, macOS, and Windows on
the [GitHub Releases page](https://github.com/fabean/BurrowTime/releases). Each
archive contains both `burrowtime` and the optional `watson` compatibility
binary.

### Agent time tracking

The BurrowTime binary contains an optional skill for coding agents. Downloading
the binary does not install or activate the skill. Install it once for your
agent:

```bash
burrowtime skill install codex
burrowtime skill install claude
burrowtime skill install cursor
burrowtime skill install gemini
burrowtime skill install opencode

# Install the shared skill plus Claude Code's copy.
burrowtime skill install all
```

Codex, Cursor, Gemini, and OpenCode share the copy under
`~/.agents/skills/track-time-with-burrowtime`. Claude Code uses
`~/.claude/skills/track-time-with-burrowtime`. The skill does not track ordinary
chats. Ask for tracking in the task prompt:

```text
Track this work in BurrowTime under "client portal" +PORTAL-42.
```

If the project or task is missing, the skill asks for it before starting work.
It checks the installed binary's agent protocol, creates an agent session,
renews its lease during long work, pauses while waiting for required input, and
stops that exact session at the end. Check the installation and the binary on
your `PATH` with:

```bash
burrowtime skill doctor codex
burrowtime skill doctor all
```

This catches an old binary paired with a newer skill. To replace local changes
with the files bundled in the current binary, run:

```bash
burrowtime skill install codex --force
```

Releases before the shared installer used
`~/.codex/skills/track-time-with-burrowtime`. The installer warns when that
legacy copy is still present, and `skill doctor` treats it as a duplicate so
you can remove it deliberately.

Agents without compatible skill discovery can use BurrowTime's MCP server.
Configure the agent to launch `burrowtime mcp` over standard input and output.
A typical MCP entry is:

```json
{
  "mcpServers": {
    "burrowtime": {
      "command": "burrowtime",
      "args": ["mcp"]
    }
  }
}
```

The MCP server provides start, heartbeat, pause, resume, stop, status, recovery,
reporting, and capability tools. The same explicit-request rule still applies.
An agent should start tracking only when you ask it to.

Package-manager recipes will be advertised here only after their first release
has been published and tested. Maintainers can follow the
[distribution guide](docs/DISTRIBUTION.md) for AUR, Homebrew, and Scoop.

### Build from source

```bash
git clone https://github.com/fabean/BurrowTime.git
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
is active, an ordinary `start` opens an interactive chooser. You can stop all
active timers and replace them with the new one, or keep them running and start
the new timer concurrently. Press Escape to leave everything unchanged.

Scripts and redirected commands never wait for the TUI. They receive the
existing `already started` error and must choose the behavior explicitly:

```bash
# Stop every active timer, then start this one.
burrowtime start --stop "client call" +meeting

# Keep existing timers running and add another.
burrowtime start --concurrent "background export" +ops

# Return the new timer ID for an agent or script.
burrowtime start --concurrent --json "background export" +ops
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

### Agent-owned sessions

Agent sessions are metadata around standard BurrowTime timers. They do not
create a second kind of billable record. A completed session still becomes an
ordinary Watson frame and appears in normal logs and reports.

Start a session directly with:

```bash
burrowtime agent start \
  --client codex \
  --project "client portal" \
  --task PORTAL-42 \
  --lease 30m \
  --json
```

The JSON result contains `session.id`. Agent integrations retain that ID and
use it for every later action:

```bash
burrowtime agent heartbeat --session <session-id> --json
burrowtime agent pause --session <session-id> --json
burrowtime agent resume --session <session-id> --json
burrowtime agent stop --session <session-id> --json
```

`agent start` accepts `--owner` for a conversation or run ID and
`--idempotency-key` for safe retries. Leases prevent an interrupted agent from
leaving an unbounded timer behind. Run `burrowtime agent recover` to close
expired sessions at their lease deadline. `burrowtime agent status` lists
sessions, and `burrowtime agent report` groups recorded time by session and
client.

You can still stop an agent's active timer with normal commands or the TUI.
`burrowtime stop` works when it is the only timer. With several timers, select
it in the picker or pass `--timer`. BurrowTime marks the session
`manually_stopped`, and a later agent stop succeeds without changing anything.

A repository can provide defaults for direct agent commands in its nearest
`.burrowtime.toml`:

```toml
[agent]
project = "client portal"
task = "PORTAL-42"
repository = "portal-app"
lease = "30m"
task_from_branch = false
```

Set `task_from_branch = true` to infer ticket-shaped values such as
`PORTAL-42` from the current Git branch when `task` is omitted. Explicit flags
take precedence. The bundled skill itself asks the user when the project or
task was not supplied, so repository defaults do not turn tracking on.

Use `burrowtime capabilities --json` to inspect the supported agent protocol,
MCP version, skill targets, session states, and individual features.

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
| `agent` | Control and report agent-owned sessions |
| `capabilities` | Show the machine-readable agent protocol |
| `mcp` | Run the BurrowTime MCP server over stdio |
| `skill` | Install and diagnose bundled agent skills |
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
- agent ownership and leases live in the additive `agent_sessions` file;
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
