# BurrowTime Porting Plan

## Recommendation

Build BurrowTime as a Go port of Watson, while keeping the existing Python
implementation at `/home/josh/Projects/Watson` as an executable compatibility
oracle until the Go version passes the full parity suite.

Go is a good fit because Watson is a local-first command-line program with a
small domain model, modest HTTP needs, and no Python-specific extension API.
It gives Watson a single binary, simple cross-compilation, fast startup, and a
small long-term dependency surface. Rust would also produce an excellent
binary, but would add implementation complexity without providing a meaningful
benefit for this workload. Continuing in Python would be the lowest-effort
maintenance option, but would retain the packaging and interpreter dependency
that motivates this port.

The port should be a behavioral rewrite, not a redesign. The first Go release
must provide `burrowtime` as its primary executable and `watson` as a
compatibility alias, with the same commands, flags, configuration, data files,
and output contracts. Storage changes or new features can follow after parity.

## Definition of compatibility

The Go binary is compatible when an existing user can replace the Python
`watson` executable and:

- Keep the existing Watson directory without importing or modifying it first.
- Read and write `config`, `frames`, `state`, and `last_sync` in the same
  locations and formats.
- Set `WATSON_DIR` and `TZ` with the same effect.
- Run every existing command with the same arguments, defaults, validation,
  exit status, stdout/stderr division, and materially identical output.
- Switch between the Python and Go binaries against the same data directory
  without data loss or semantic drift.
- Continue using abbreviated frame IDs, negative frame indexes, editors,
  pagers, colors, confirmations, completion, merge files, and the HTTP sync
  protocol.
- Receive the same JSON and CSV schemas and stable field ordering, because
  those formats may be consumed by scripts.

The shipped `watson` compatibility executable retains that direct replacement
behavior. The primary `burrowtime` executable now defaults to its own platform
data directory and `BURROWTIME_DIR`; it copies the same compatible files in or
out through the first-run migration flow and `burrowtime migrate`. This keeps
trying BurrowTime from mutating an existing Watson installation.

Byte-for-byte equality is required for persisted files and machine-readable
output after normalization of intentional nondeterminism such as generated
UUIDs and the current time. Human-readable help text may differ only where the
Go command framework makes exact reproduction disproportionately costly; all
documented names, aliases, flags, and behavior remain fixed.

## Existing contract to preserve

### Files and data model

Watson resolves its application directory using the platform convention, with
`WATSON_DIR` as an override. It stores these extensionless files:

| File | Existing representation | Compatibility requirement |
| --- | --- | --- |
| `frames` | JSON array of six-element arrays: `[start, stop, project, id, tags, updated_at]` | `start`, `stop`, and `updated_at` are Unix seconds; `stop` may be null; preserve array order, Unicode, IDs, tags, and second precision. |
| `state` | JSON object containing `project`, `start`, and `tags`, or `{}` | `start` is a Unix timestamp; missing tags behave as an empty list. |
| `last_sync` | JSON integer | Treat missing, empty, or zero as the Unix epoch. |
| `config` | Python `RawConfigParser`-style INI | Preserve sections, case-insensitive option lookup, `=`/`:` delimiters, comments, continuations, quoted list values, and Watson's boolean rules. |

Writes currently go to a temporary file, rotate the destination to `.bak`, and
move the completed temporary file into place. The Go implementation must keep
that recoverability contract and should add `fsync` plus a same-directory
temporary file so rename is atomic on the destination filesystem. It must not
silently rewrite a file merely because it was read.

The `watson` compatibility executable and Python Watson may share a Watson
directory sequentially. BurrowTime normally uses its own directory and copies
compatible data between them explicitly. Running two mutating processes
concurrently against an explicitly shared directory is not safe.

### Commands

Preserve all current commands:

- Core timer: `start`, `stop`, `restart`, `cancel`, `status`.
- History mutation: `add`, `edit`, `remove`, `rename`.
- Query and export: `log`, `report`, `aggregate`, `projects`, `tags`, `frames`.
- Administration: `config`, `merge`, `sync`, `help`, version output.

Command parity includes multi-word projects and tags, `+tag` parsing, default
tags, confirmation settings, stop-on-start/restart, gap/no-gap behavior,
partial-frame clipping in reports, filters and ignore filters, current-frame
inclusion, reverse logs, date shortcuts (`day`, `week`, `month`, `year`,
`all`, and `luna`), custom week starts, pager selection, JSON/CSV/plain output,
and mutually exclusive flag errors.

### Semantics that need explicit fixtures

- Local time and `TZ`, including daylight-saving transitions and ambiguous or
  nonexistent local times.
- Accepted date inputs: ISO-like values and time-only `HH:mm[:ss]` values.
- Inclusive report spans and Arrow's `floor`/`ceil` behavior.
- Python `strftime` configuration translated to Go time formatting.
- Arrow-style humanized durations in `status`, `start`, `stop`, and `add`.
- Tag filtering uses “any requested tag,” not “all requested tags.”
- Frame lookup uses negative indexes or an ID prefix. Existing ambiguous ID
  prefixes resolve to the first stored frame; preserve this initially and add
  a future warning only in a separate release.
- `updated_at` controls sync selection and is refreshed by renames/edits.
- JSON indentation, CSV headers, line endings, sort order, short IDs, and
  stable report ordering.
- The bundled full-moon timestamp table and its supported range.

## Proposed Go architecture

Keep dependencies few and keep domain behavior independent from the CLI:

```text
cmd/burrowtime/         primary process entry point
internal/cli/           command definitions, prompts, output, completion
internal/watson/        timer and reporting use cases
internal/frame/         Frame, collection, filtering, spans
internal/store/         paths, JSON codecs, atomic writes, backups
internal/config/        Watson-compatible INI and list parsing
internal/datetime/      input parsing, spans, formatting, humanization
internal/report/        report/log aggregation and output models
internal/sync/          HTTP protocol and merge behavior
internal/testutil/      clock, UUID, filesystem, and process fixtures
testdata/compat/        captured inputs, data directories, and golden output
```

Use `time.Time` internally and explicitly convert persisted timestamps through
UTC Unix seconds. Inject a clock and UUID generator into use cases so tests are
deterministic. Keep rendering outside the domain layer, and have all mutating
commands call one storage transaction boundary.

Use Cobra for command routing and generated Bash/Zsh/Fish/PowerShell
completion. Avoid Viper: its configuration behavior is broader than Watson's
and is unlikely to reproduce Python `RawConfigParser` exactly. Either select a
small INI parser only after passing compatibility fixtures or implement the
narrow parser Watson needs. Implement Watson's Python-format-to-Go-format
translation and humanization locally; general date libraries are unlikely to
match Arrow closely enough at the edges.

## Execution plan

### Phase 0: Freeze and protect the reference behavior

1. Tag the Python baseline used for the rewrite and record its version and
   commit.
2. Make a redacted copy of a real long-lived Watson directory for validation.
   Never run development binaries against the only copy of user data.
3. Restore the Python test environment and establish a green baseline for the
   existing 122 tests. Record any failures rather than changing behavior to
   make the port cleaner.
4. Add CI for the Python baseline while it remains the oracle.

Exit criterion: the legacy program is reproducible and representative test
data can be restored after any failed experiment.

### Phase 1: Build the compatibility specification and harness

1. Turn current tests, command documentation, and discovered edge cases into a
   command/flag parity matrix.
2. Add a black-box runner that executes Python Watson and BurrowTime with the
   same temporary `WATSON_DIR`, fixed clock/UUID inputs, locale, terminal
   width, color setting, and `TZ`.
3. Capture exit code, stdout, stderr, and all four resulting files. Normalize
   only known nondeterministic values.
4. Create fixtures for empty/missing/corrupt files, legacy five-field frames
   without `updated_at`, Unicode, quotes and multiline INI values, DST,
   partial frames, merge conflicts, failed writes, and HTTP sync responses.
5. Add round-trip tests in both directions: Python writes/Go reads and Go
   writes/Python reads.

Exit criterion: every compatibility promise has an automated assertion or an
explicitly documented manual test.

### Phase 2: Scaffold the Go program

1. Add the Go module, `cmd/burrowtime`, the `watson` compatibility entry point,
   internal packages, version injection, and Make targets without changing the
   Python reference repository.
2. Add `go test`, formatting, vet/static analysis, and builds for Linux,
   macOS, and Windows in CI.
3. Define typed errors and map them centrally to stable user messages and exit
   codes.
4. Implement dependency injection for clock, UUIDs, environment, terminal,
   editor, pager, and HTTP client.

Exit criterion: a minimal binary builds reproducibly on all target platforms
and the differential harness can invoke it.

### Phase 3: Implement persistence and configuration first

1. Implement platform application-directory resolution, `BURROWTIME_DIR`, and
   the `WATSON_DIR` behavior retained by the compatibility executable.
2. Implement strict codecs for frames, state, and last-sync, including missing
   files, empty files, legacy omitted fields, invalid JSON, Unix seconds, and
   Unicode.
3. Implement atomic write/backup behavior with failure-injection tests.
4. Implement the INI subset and Watson getters: string, int, float, boolean,
   and shell-like/multiline list parsing.
5. Prove no-op reads do not modify bytes or timestamps; prove write results
   can immediately be read by Python Watson.

Exit criterion: all storage and configuration cross-read/round-trip fixtures
pass before any command is allowed to mutate real data.

### Phase 4: Port the domain model

1. Implement frames, prefix/index lookup, sorting, spans, overlap/clipping,
   projects, tags, and deduplication.
2. Implement start/stop/cancel/restart/add invariants and default-tag behavior.
3. Implement reports, aggregation, tag totals, current-frame projection, and
   rename operations.
4. Port the full-moon table and boundary behavior.
5. Unit-test the domain with fixed clocks in UTC and several local zones.

Exit criterion: domain-level fixtures match Python results without involving
the CLI renderer.

### Phase 5: Port read-only CLI commands

Implement `status`, `projects`, `tags`, `frames`, `log`, `report`, and
`aggregate`, followed by help/version behavior. Match plain, JSON, and CSV
output; date/time formatting; color detection; paging; filtering; sorting; and
shortcut ranges.

Start with read-only commands because they can safely be exercised on copied
real data and expose most date/reporting incompatibilities before writes are
enabled.

Exit criterion: differential tests pass for all read-only commands across the
fixture corpus and the redacted real dataset.

### Phase 6: Port routine mutating commands

Implement `start`, `stop`, `cancel`, `restart`, `add`, `remove`, `rename`, and
`config`. Test every command as a state transition: initial files, invocation,
output, final files, backup files, then a read by the Python implementation.

Exit criterion: repeated Python/Go alternation produces the same logical data
and neither implementation rejects the other's output.

### Phase 7: Port interactive and integration features

1. Implement `edit`, honoring `VISUAL` before `EDITOR`, temporary JSON shape,
   validation/retry behavior, and current-frame editing.
2. Implement `merge`, including statistics, prompts, conflict highlighting,
   force behavior, IDs, and `updated_at` preservation.
3. Implement `sync` with the existing URLs, token header, UUID URNs, UTC
   timestamps, last-sync selection, error handling, and request/response
   status expectations. Use a local fake HTTP server in tests.
4. Generate completion and add dynamic completion for projects, tags, frame
   IDs, and rename types.

Exit criterion: pseudo-terminal tests cover prompts/editor/pager behavior and
the fake server proves wire compatibility.

### Phase 8: Package and cut over safely

1. Produce versioned archives and checksums for Linux, macOS, and Windows;
   add a Homebrew formula and documented `go install` path if desired.
2. Run a shadow period where the Go binary performs read-only commands against
   copied production data and its output is compared with Python.
3. Run a canary period using Go for normal work while retaining the previous
   Python executable and automatic `.bak` files.
4. Publish a release candidate only after the full parity matrix passes. The
   rollback is replacing the binary; no data migration should be necessary.
5. Keep the Python implementation and parity suite for at least one stable Go
   release. Remove it only after real-world interchange has been demonstrated.

Exit criterion: the Go binary is the default, rollback has been rehearsed, and
the same untouched Watson directory works before and after rollback.

## Test strategy and release gates

Every phase adds tests at four levels:

1. Unit tests for pure parsing, time, filtering, and rendering behavior.
2. Golden tests for files and command output.
3. Differential tests against the frozen Python executable.
4. End-to-end tests that alternate binaries over one temporary data directory.

The initial stable Go release is blocked by any of the following:

- A Python-readable file becomes unreadable or changes meaning in Go.
- A Go write cannot be read by the frozen Python baseline.
- A crash or interrupted write can destroy both the destination and backup.
- Machine-readable output or sync wire data differs unexpectedly.
- Time results differ around a supported DST, timezone, range-boundary, or
  partial-frame case.
- A documented command, option, environment variable, or config key is absent.

Fuzz the JSON codecs, INI parser, tag parser, abbreviated IDs, and datetime
parser. Add regression fixtures for every discovered mismatch rather than
special-casing it only in implementation code.

## Deliberate non-goals for the parity release

- No database, schema migration, daemon, GUI, or cloud service.
- No change from integer Unix seconds to nanoseconds or formatted timestamps.
- No command renaming or “cleaned up” flags/output.
- No removal of `sync` or `luna`, even if rarely used.
- No simultaneous multi-process write guarantee.
- No new configuration format.
- No opportunistic fixes to legacy quirks unless required to prevent data
  loss. Track desirable changes for a later major release.

## Rough delivery shape

For one experienced Go developer, expect roughly four to six focused weeks:
one week for the behavior inventory and harness, one week for storage/domain,
one to two weeks for CLI/report parity, one week for interactive/sync features,
and one week for hardening and packaging. The estimate should be revised after
Phase 1; exact Arrow date parsing, humanization, and INI compatibility are the
largest uncertainty.

The safest first milestone is not `watson start`. It is a Go binary that can
read a copy of years of Watson data and produce matching `projects`, `tags`,
`frames`, `log --json`, and `report --json` output without changing a byte.
