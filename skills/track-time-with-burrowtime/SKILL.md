---
name: track-time-with-burrowtime
description: Track agent work in BurrowTime when the user explicitly asks to track, log, or clock time for the current task. Do not use merely because ordinary work mentions time, projects, or tickets.
---

# Track time with BurrowTime

Track only after the user explicitly asks to track, log, or clock the current work with BurrowTime. Installing or loading this skill does not authorize automatic tracking.

## Get the tracking details

Require a BurrowTime project and a task or ticket. Preserve the spelling the user supplies. The agent-session command accepts the task with or without a leading `+`.

If either value is missing, ask: "How do you want me to track your time? Please provide a BurrowTime project and task or ticket." Do not start a timer or begin substantive task work until the user answers.

## Verify compatibility

Before starting, use the `burrowtime_capabilities` MCP tool when available and read its `capabilities` object. Otherwise run `burrowtime capabilities --json` and read the top-level object.

Require `agent_protocol` to be at least `1` and `features.agent_sessions` to be `true`. If the check fails, stop and tell the user to update the BurrowTime binary and run `burrowtime skill doctor <client>`. Never retry with `burrowtime start`, remove JSON flags, parse human output, or otherwise fall back to an older command.

## Identify the client

Use the host agent's lowercase name as the client, such as `codex`, `claude`, `cursor`, `gemini`, or `opencode`. Use `agent` only when the host cannot be identified. If the host exposes a stable conversation or run identifier, use it as both the owner and the basis of a stable idempotency key for this tracked task. Do not invent an identifier presented as a real host ID.

## Track the work

Prefer the BurrowTime MCP tools when available:

1. Call `start_time` with `client`, `project`, `task`, a `30m` lease, and optional owner and idempotency metadata.
2. Retain the exact `session.id` returned for this work.
3. Call `heartbeat_time` with that session ID before its lease expires during long uninterrupted work.
4. Call `pause_time` before waiting for required user input. Call `resume_time` with the same session ID when the work continues.
5. Call `stop_time` with the exact session ID before the final response, or before abandoning or replacing the task.

When MCP tools are unavailable, use their CLI equivalents with safely quoted arguments:

```sh
burrowtime agent start --client <client> --project <project> --task <task> --lease 30m --json
burrowtime agent heartbeat --session <session-id> --json
burrowtime agent pause --session <session-id> --json
burrowtime agent resume --session <session-id> --json
burrowtime agent stop --session <session-id> --json
```

Include `--owner` and `--idempotency-key` on `agent start` when stable identifiers are available. Parse the JSON response and retain `session.id`.

Agent sessions use ordinary BurrowTime timers. A user may stop one manually with the normal `burrowtime stop` flow. A later agent stop is idempotent and should be treated as success when the session is already terminal.

Never use bare `burrowtime stop`, `burrowtime stop --all`, or a timer selected only by project or tag on the agent's behalf. Do not stop or modify sessions this agent did not start. If an operation fails, report the session ID and error so the user can resolve it.
