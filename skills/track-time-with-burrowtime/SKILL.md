---
name: track-time-with-burrowtime
description: Track agent work in BurrowTime when the user explicitly asks to track, log, or clock time for the current task. Do not use merely because ordinary work mentions time, projects, or tickets.
---

# Track time with BurrowTime

Start a timer only after the user explicitly asks to track the current work with BurrowTime. Installing or loading this skill does not authorize automatic time tracking.

## Get the tracking details

Require a BurrowTime project and at least one task or ticket tag. Preserve names and ticket spelling supplied by the user. Add the required `+` prefix to a tag when it is omitted.

If either value is missing, ask: "How do you want me to track your time? Please provide a BurrowTime project and task or ticket." Do not start a timer or begin substantive task work until the user answers.

## Track the work

1. Before substantive work, run `burrowtime start --concurrent --json <project> +<task>` with the project and each tag passed as separate, safely quoted arguments.
2. Parse the JSON response and retain its `id` for this tracked work. If the command fails, tell the user that tracking did not start. Continue only if the user did not make successful tracking a condition of the work.
3. Complete the requested work.
4. Before the final response, or before abandoning or replacing the task, run `burrowtime stop --timer <id>` using the retained ID.

Always stop the recorded timer. Never use a bare `burrowtime stop`, `burrowtime stop --all`, or a timer selected only by project or tag. Do not stop timers that this agent did not start.

If work must pause for required user input, stop the timer before asking. When the user responds and the same tracked task resumes, start a new timer with the same project and tags before continuing.

If stopping fails, report the timer ID and error so the user can resolve the running timer manually.
