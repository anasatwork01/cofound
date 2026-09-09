# Sandbox image

`Dockerfile` plus Modal image definitions for the agent sandbox. Built per
template version and pre-baked so that time-to-first-preview stays inside the
SLO in SPEC §17.3 (p50 < 10s).

Contents the image must provide: Node + pnpm, git, ripgrep, the built
`opencode` binary, `agentd`, and the template's dependencies already installed.

Task 1.2.
