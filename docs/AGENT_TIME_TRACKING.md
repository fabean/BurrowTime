# Time tracking for coding agents

BurrowTime is an open-source, local-first time tracker for Codex, Claude Code,
Cursor, Gemini CLI, OpenCode, and other MCP clients. Agent integrations track
only the work a user explicitly asks them to track.

Agent sessions use normal BurrowTime timers. The extra session record adds an
owner, a lease, safe retry metadata, and pause or resume state. When the work
ends, its time appears in the same logs and reports as any timer you start
yourself.

![A BurrowTime agent session being started, paused, resumed, stopped, and reported](../assets/burrowtime-agent.gif)

## Quick setup

Install BurrowTime with Go 1.24 or newer:

```bash
go install github.com/fabean/BurrowTime/cmd/burrowtime@latest
```

Install the bundled skill for your agent:

```bash
burrowtime skill install codex
burrowtime skill install claude
burrowtime skill install cursor
burrowtime skill install gemini
burrowtime skill install opencode

# Install the portable copy plus the Claude Code copy.
burrowtime skill install all
```

Check that the skill and the binary agree on the agent protocol:

```bash
burrowtime skill doctor codex
burrowtime skill doctor all
```

Then name the project and task in your request:

```text
Track this work in BurrowTime under "client portal" +PORTAL-42.
```

Installing the skill never starts a timer. The agent must see an explicit
tracking request with both a project and task before it can begin.

## What a well-behaved integration does

1. Check `burrowtime capabilities --json` and require `agent_protocol` 1 with
   `features.agent_sessions` enabled.
2. Start an agent session with a client, project, task, and 30-minute lease.
3. Retain the exact `session.id` returned by BurrowTime.
4. Renew the lease before it expires during long work.
5. Pause before waiting for required user input, then resume the same session.
6. Stop that exact session before the final response.

The integration must not use bare `burrowtime stop`, `burrowtime stop --all`,
or a timer selected only by project or tag. Those commands can affect work the
agent does not own.

## MCP configuration

Any client that can launch a local MCP server can use BurrowTime over standard
input and output:

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

The server provides `start_time`, `heartbeat_time`, `pause_time`,
`resume_time`, `stop_time`, session listing and recovery, agent reports, and a
capability check. It does not open a network port or contact a hosted service.

## Direct CLI integration

Clients without MCP can use the JSON commands directly:

```bash
burrowtime agent start \
  --client codex \
  --project "client portal" \
  --task PORTAL-42 \
  --lease 30m \
  --json

burrowtime agent heartbeat --session <session-id> --json
burrowtime agent pause --session <session-id> --json
burrowtime agent resume --session <session-id> --json
burrowtime agent stop --session <session-id> --json
```

Pass a stable conversation or run identifier with `--owner`, and use a stable
retry key with `--idempotency-key`. A repeated start with the same client and
key reuses the session.

## Repository defaults

Direct commands read the nearest `.burrowtime.toml`:

```toml
[agent]
project = "client portal"
task = "PORTAL-42"
repository = "portal-app"
lease = "30m"
task_from_branch = false
```

Set `task_from_branch = true` to infer ticket-shaped values such as
`PORTAL-42` from the current branch when no task is configured. These values
provide context. They do not authorize tracking.

## Reports and recovery

```bash
burrowtime agent status --active
burrowtime agent report --project "client portal"
burrowtime agent report --client codex --json
burrowtime agent recover
```

A user can stop an agent timer with the normal CLI or terminal UI. BurrowTime
marks the agent session as manually stopped, and the agent's later cleanup is
safe to repeat.

The complete web guide is at
[fabean.github.io/burrowtime-site/docs/agent-time-tracking](https://fabean.github.io/burrowtime-site/docs/agent-time-tracking/).
