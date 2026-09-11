# Sandbox image

`Dockerfile` plus Modal image definitions for the agent sandbox. Built per
template version and pre-baked so that time-to-first-preview stays inside the
SLO in SPEC §17.3 (p50 < 10s).

**That SLO is not yet known to be achievable, and this image is not the reason
why.** Task 1.1 established that Modal publishes no timing at all for snapshot
create or restore — only a ~0.5s median for container boot — and the one hard
signal is that the SDK's default snapshot timeout is 55s with support added for
longer, so a snapshot _can_ exceed 55 seconds. Task 1.19 measures it. Until
then, treat the pre-baked image as removing the dependency-install cost, which
is the part we control, and do not assume the remaining budget fits.

Use `Image.from_name('<template>:<version>')` with `Image.publish()` rather than
a per-create image build, and record the published name and tag in the template
row. See `docs/verified.md` §22 item 5.

Contents the image must provide: Node + pnpm, git, ripgrep, the built
`opencode` binary, `agentd`, and the template's dependencies already installed.

Task 1.2.
