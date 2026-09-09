# App templates

Each template is a git repo with pinned dependency versions and a pre-baked
sandbox image per version. Templates must ship secure defaults, the analytics
collector, cookie consent and a permissive explicit license (SPEC §19.1).

Template-level decisions that cannot be made per project:

- the Postgres driver used from Cloudflare Workers (Hyperdrive vs HTTP driver)
- the auth library and its pinned version (SPEC §21 decision 4)
- Cloudflare cron trigger declarations

Task 2.1.
