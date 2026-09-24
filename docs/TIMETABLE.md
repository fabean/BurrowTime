# Timetable connector

Export completed BurrowTime entries to [Timetable](https://timetable.bluedroplabs.com/) using its [documented API](https://timetable.bluedroplabs.com/api-docs/). Each local frame gets a stable `source=burrowtime` and `external_id`, so retrying a request whose response was lost does not create another Timetable entry.

## Install

Install both BurrowTime and the optional connector on your `PATH`:

```sh
go install github.com/fabean/BurrowTime/cmd/burrowtime@latest
go install github.com/fabean/BurrowTime/cmd/burrowtime-timetable@latest
```

From a checkout, use `make install` and `make install-timetable`. Release archives include both executables.

## Configure

Create a personal token in Timetable Settings. Find your user ID from `GET /api/me` using that token. Keep the token in an environment variable; BurrowTime saves only the variable name.

```sh
export TIMETABLE_TOKEN='your-personal-token'
burrowtime timetable configure --user YOUR_TIMETABLE_USER_ID
burrowtime timetable projects
burrowtime timetable map 'local project' TIMETABLE_PROJECT_UUID
```

The default site is `https://timetable.bluedroplabs.com`. For another deployment, pass `--url https://your-timetable-host` to `configure`. Use `--token-env NAME` if the token is stored in another environment variable. `--connection NAME` gives a connection its own name. The same local project can be mapped to both Timetable and Clockify; their destinations and receipts remain separate.

Timetable keeps exact seconds. Rounding defaults to `off`; `configure --rounding up --increment 15m` or `map ... --rounding off` works like Clockify's per-entry settings. Timetable has no billable field in this API, so this connector does not send one.

## Preview and sync

```sh
burrowtime timetable sync --today --dry-run
burrowtime timetable sync --today
burrowtime timetable sync --from 2026-09-01 --to 2026-09-09
burrowtime timetable sync --all --yes
burrowtime timetable status
```

`sync` defaults to today in local time. In a terminal it offers project mapping and a review screen before upload. For unattended use, configure mappings first, inspect `--dry-run`, and pass `--yes`. Dry runs do not write receipts or upload entries; an interactive dry run may read your user and project list.

The local receipt is saved before each upload. If a response is lost, `status` shows a pending entry. Rerun `sync` with the same mapping and source entry; Timetable returns the existing entry for the same import identity and body. If Timetable returns a conflict, compare the remote entry and local receipt before changing either. Once synced, changing an already exported source entry is blocked so another time entry is not created silently.

Only completed entries are exported. Descriptions contain sorted BurrowTime tags. The original local frame stays unchanged.
