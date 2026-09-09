# Cloudflare infrastructure

`wrangler` configs and Terraform for: the console Worker, per-project app
Workers, Cloudflare for SaaS custom hostnames, R2 buckets, Workers KV
(hostname -> deployment routing), Queues, Hyperdrive, WAF and DNS.

See SPEC §3.5 for what can and cannot run on Workers. **Do not attempt to port
`gitd` or `aigw` to Workers** - long-lived streaming and git packfile handling
are the wrong shape for that runtime.
