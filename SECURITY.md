# Security policy

## Supported versions

Security fixes are made on the current default branch and included in the next
tagged release. Users should run the latest release.

## Report a vulnerability

Please use GitHub's private vulnerability reporting for this repository. Do
not open a public issue with exploit details, private paths, or user data.

[Report a vulnerability privately](https://github.com/fabean/BurrowTime/security/advisories/new)

Include the affected version, operating system, reproduction steps, and the
impact you observed. A minimal reproduction using a temporary data directory
is especially helpful.

BurrowTime stores local time data and can be launched by coding agents. Reports
that involve unsafe file access, command execution, session ownership, MCP
input handling, or accidental modification of unrelated timers are in scope.
