# Clockify connector

Export completed BurrowTime entries to Clockify with project mapping, optional
per-entry rounding, an editable review, and local upload receipts. The connector
is installed separately. It runs only when you invoke it through BurrowTime.

## Install from this checkout

```bash
go install ./cmd/burrowtime
go install ./cmd/burrowtime-clockify
```

To install without cloning, use the latest tagged release:

```bash
go install github.com/fabean/BurrowTime/cmd/burrowtime@latest
go install github.com/fabean/BurrowTime/cmd/burrowtime-clockify@latest
```

The plugin executable must be on `PATH`. `make install` continues to install
only BurrowTime and Watson; `make install-clockify` installs the connector.
Release archives include the connector alongside the main executables.

## Configure

Provide `CLOCKIFY_API_KEY`, `CLOCKIFY_WORKSPACE_ID`, and `CLOCKIFY_USER_ID` in
your shell environment or through your secret manager. No key is written to configuration
or receipts. This connector exports as the API key's owner and verifies that
the configured user ID matches.

```bash
burrowtime clockify configure --rounding up --increment 15m
burrowtime clockify sync --today
```

You can also pass `--workspace ID`, `--user ID`, and `--api-key-env VARIABLE`
to configure. Reconfiguring an existing connection preserves unspecified fields.
Rounding is disabled by default, so explicitly select `up` to enable it.

## Map and review interactively

When sync encounters an unmapped local project, it shows that project's name
and a full-screen Clockify project picker styled like the BurrowTime dashboard.
Each project shows its client name beneath it. Search includes client names;
the review and editor show the client alongside the selected project.
Normalized exact names, contained names,
shared words, and similar spelling determine suggestion order. Suggestions
never select a destination without your input.

- Use arrow keys or `j`/`k` to move; Enter selects the highlighted project.
- Use Page Up and Page Down to browse longer lists.
- Press `/` to search project names, clients, or IDs as you type. Enter returns to browsing;
  Escape leaves search, and Escape again clears the filter.
- Use `s` to skip this local project for the current run.
- Use `q` or close the input to cancel without uploading.

Next, review the descriptions, Clockify destinations, start/end timestamps,
recorded and exported durations, billable settings, and batch totals.

- Arrow keys or `j`/`k` select an entry. Enter or `e` opens its editor.
- In the editor, `p` changes the project, `d` the description, `t` the exported
  duration, and `b` toggles billable. Enter saves a text field; Escape discards
  that field edit. Ctrl+U clears its contents.
- `s` omits the selected entry from this batch.
- `p` opens upload confirmation; `y` explicitly approves it. Escape returns
  to the review without uploading.
- `q` or Ctrl+C cancels sync. Escape cancels from the review screen.

Dry-run uses the same mapping and editable review screens, with a visible
DRY RUN label. Press `p` to finish that preview; it never offers an upload.

Review edits affect only that export. They do not alter local frames or the
project's default mapping. Descriptions contain only the tags or
your edited text. An edited duration overrides rounding for that upload. New mappings
are saved when you confirm a nonempty batch. Skipped entries remain eligible
for a future sync.

You can map projects directly instead:

```bash
burrowtime clockify projects
burrowtime clockify map "client portal" CLOCKIFY_PROJECT_ID --billable
burrowtime clockify map "personal work" ANOTHER_PROJECT_ID --rounding off
```

## Rounding

Rounding applies to each completed entry's duration, not the daily total or
its clock start time. Modes are `off`, `up`, and `nearest`. Nearest rounds ties
up; if it would produce zero seconds, the entry is blocked for correction.

With `up` and `15m`, 16 minutes exports as 30 minutes, while exactly 15 minutes
stays 15. Three two-minute entries each export as 15 minutes. The remote end is
the original start plus the exported duration. Adjacent entries can therefore
overlap, and an entry close to midnight can extend into the following day.
The review shows those timestamps before uploading.

The local record always retains the exact original duration. Clockify consumers
that calculate hours from start/end timestamps see the exported duration.
Tags appear in the description without their `+` prefixes. For example,
`burrowtime start sema +SEMA-123` exports the description `SEMA-123`.
Multiple tags are sorted and joined with spaces; no tags produces an empty
description, which you can edit during review. Local project names are not
included in the description. BurrowTime IDs
stay in the local ledger and are not appended to Clockify descriptions.
Native Clockify tag/task mappings are not yet supported.

## Dates and automation

Sync selects entries by their original start timestamp. Today uses the
computer's local timezone; an entry started yesterday belongs to yesterday,
even if stopped today. Date ranges include both named dates. Entire completed
entries are exported, without clipping or daily splitting.

```bash
burrowtime clockify sync --today --dry-run
burrowtime clockify sync --from 2026-09-01 --to 2026-09-09
burrowtime clockify sync --all --dry-run
```

Outside an interactive terminal, mappings must already exist and uploads require
`--yes`. Missing mappings fail before any upload. `--yes` approves the generated
batch without editing it. Running timers are never exported.

```bash
burrowtime clockify sync --today --yes
```

Dry-run never uploads or saves receipts or mappings. In a terminal it may fetch
project suggestions; a noninteractive dry-run needs no plugin or API access.

## Multiple connections

Use `--connection NAME` on any subcommand. Each local project routes to one
connection. Projects mapped to other connections are excluded from the batch.

```bash
burrowtime clockify configure --connection freelance \
  --workspace WORKSPACE_ID --user USER_ID --api-key-env FREELANCE_CLOCKIFY_KEY
burrowtime clockify map "freelance project" PROJECT_ID --connection freelance
burrowtime clockify sync --connection freelance
```

`integrations.json` lives in BurrowTime's data directory, alongside the separate
`integration-sync.json` receipt ledger. Both honor `--data-dir` and
`BURROWTIME_DIR`. The existing Watson configuration and six-field frame format
are unchanged. Example configuration:

```json
{
  "version": 1,
  "connections": {
    "clockify": {
      "plugin": "clockify",
      "workspace_id": "workspace-id",
      "user_id": "user-id",
      "api_key_env": "CLOCKIFY_API_KEY",
      "rounding": { "mode": "up", "increment": "15m" }
    }
  },
  "projects": {
    "client portal": {
      "connection": "clockify",
      "project_id": "project-id",
      "billable": true
    }
  }
}
```

## Receipts and recovery

```bash
burrowtime clockify status
```

Before a create request, BurrowTime saves a pending receipt. After success it
saves the remote ID immediately. Reruns skip successful entries, including
entries edited during review. Partial failures retain earlier successes.
Local edits, changed mappings, or changed rounding after export are flagged;
this first version does not update or delete remote entries.

A failed request can mean the response was lost after Clockify created the
entry. Such receipts remain pending and block automatic retries. Locate the
entry in Clockify by comparing its project, description, and exact start/end
timestamps against `burrowtime clockify status`, then:

```bash
burrowtime clockify resolve FRAME_ID CLOCKIFY_ENTRY_ID
```

Resolve verifies the remote user, project, description, billable setting, and
timestamps against the saved export before marking it synced. If you have
verified that Clockify did not create the entry, explicitly allow a retry:

```bash
burrowtime clockify retry FRAME_ID --confirmed-not-created
burrowtime clockify sync --all
```

Only pending receipts can be reset. Incorrectly confirming absence can create
a duplicate; use resolve for an entry that already exists.

Receipts from older versions that included a `[burrowtime:ID]` suffix remain
valid and prevent repeat uploads. Upgrading does not edit existing Clockify
descriptions or alter the saved payload needed to resolve an old pending upload.

Back up the ledger with your local data. Deduplication is local to this data
directory, not distributed across computers. Deleting the ledger or exporting
the same frames from another machine without it can duplicate time. Use one
exporting machine for a shared history. Corrupt ledgers fail closed.

An exclusive `integration-sync.lock` prevents concurrent syncs and configuration
changes. A killed process may leave this lock behind. Verify that no sync is
still running before removing that specific lock file; keep the receipt ledger.

## Connector protocol

The optional executable receives one protocol-version-1 JSON request on stdin
and returns one JSON response on stdout, then exits. Diagnostics must go to
stderr. BurrowTime bounds responses and execution time. Operations are `check`,
`projects`, `create`, and `get`; the types are in
`internal/integrations/protocol.go`. Keys are read from the configured environment
variable. Plugins run as the local user and should only be installed from trusted
sources.

The core owns mappings, rounding, review, locking, and receipts. The executable
owns HTTP translation. Clockify and Timetable are registered; each has its own
command and adapter. See [Timetable setup](TIMETABLE.md) for that connector.
Jira and Tempo are not part of this connector.

The implementation uses the [Clockify API](https://docs.clockify.me/), with
`X-Api-Key`, `/user`, paginated workspace projects, and completed time entries.
The default global API is used. Workspaces requiring custom time-entry fields
or native task IDs need additional connector support before exporting.
