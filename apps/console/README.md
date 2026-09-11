# Console

The Halyard SaaS frontend: Next.js App Router, TypeScript, Tailwind with a token
layer, TanStack Query, Zustand, deployed to Cloudflare Workers via
`@opennextjs/cloudflare`.

Route structure is fixed by SPEC §18 and all twelve routes exist. Streaming is
SSE only - no WebSockets in v1. The editor will be CodeMirror 6, not Monaco
(task 2.7).

Task 0.11 built the shell. Each screen carries a comment naming the task that
fills it in.

## Before changing a version here

`docs/verified.md` §22 item 1 is not optional reading. The short version:

- **`next` and `@opennextjs/cloudflare` are a matched pair, pinned exactly.**
  Next 15 stops passing the adapter's build gate on 2026-10-21, and a major
  missing from the adapter's table (17, when it ships) fails the same way. A
  caret on `next` would let a routine update break the build with an error that
  reads like a policy complaint.
- **No `export const runtime = "edge"`.** The adapter targets Next's Node.js
  runtime deliberately.
- **No Node.js middleware.** Supported only as an experimental, unmaintained
  path. Edge middleware is fine.
- **`global_fetch_strictly_public` in `wrangler.jsonc` is a security flag**, not
  a functional one. Removing it would let a server-side fetch to one of our own
  zones bypass the WAF and any Access policy (SPEC §17).
- **A dependency that imports `node:child_process`, `worker_threads` or
  `sqlite` will import fine and throw on the first request.** Refuse it at
  review rather than discover it in staging.

## Local

```bash
nvm use                # 22.18.0; engine-strict rejects older
pnpm --filter @halyard/console dev        # ordinary next dev
pnpm --filter @halyard/console preview    # opennext build + wrangler dev
```

`next dev` is the inner loop; `preview` is a pre-merge check.
