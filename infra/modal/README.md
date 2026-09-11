# Modal infrastructure

Sandbox image definitions, warm-pool configuration, project-state snapshots and
the SEO crawl function (Playwright + Lighthouse).

**Not volume layout, and that is a forced choice rather than a preference.**
`volumes=` is a `Sandbox.create` parameter only, so a pre-booted pooled sandbox
can never have a project's Volume attached at claim time - Volumes and a warm
pool are mutually exclusive. Project state is therefore a **directory
snapshot**, mounted into the running sandbox with `mount_image()`. Volume v1's
50,000-file comfort budget and 500,000-inode hard ceiling against a
`node_modules` settle it the same way. A Volume here is a rebuildable cache
(pnpm store, build artifacts) or it is not used. See `docs/verified.md` §22
item 5.

Two more constraints this directory has to encode, both verified:

- **`agentd` is the sandbox entrypoint (CMD), not an `exec`.** A
  `ContainerProcess` handle cannot be reattached from another `sandboxd`
  replica, and only the entrypoint's logs are stored by Modal.
- **A running process does not keep a sandbox alive.** Modal counts only a
  running exec, stdin writes, or an open tunnel connection as activity, and
  `idle_timeout` has no pre-termination hook - so it must never be the
  15-minute snapshot trigger, only a wide backstop.

## Egress

Deny-by-default (SPEC §9): allow only package registries, `aigw`, `gitd`,
`mcp`, and the project's own app database host. Modal supports this directly
via `outbound_cidr_allowlist` and `outbound_domain_allowlist`.

This is the file where the allowlist actually gets written, so its ceiling
belongs here rather than being rediscovered by a sandbox that cannot reach its
own database:

- **Domain matching is TLS/443 SNI only.** Modal does not decrypt, so the Host
  header and path are never inspected. **The app database on 5432 needs a CIDR
  entry**, not a domain entry.
- **Domain fronting is a documented bypass.** A shared-CDN allowlist entry is
  an exfiltration channel, and `*.github.com` grants every repo on GitHub - not
  just the project's.
- **Always pass BOTH allowlists.** Passing one leaves the other as an empty
  list, silently killing all non-443 egress. The "If None, all CIDRs are
  allowed" docstring only holds when both are None.
- **Prefer an empty allowlist to `block_network=True`**, which also disables
  i6pn and is mutually exclusive with all three allowlist parameters.
- **`outbound_domain_allowlist` is Beta and three months old.** Pin the client,
  and assert in an integration test that a blocked destination actually fails -
  a Modal-side behaviour change here is a security regression, not a bug.
