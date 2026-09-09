# Modal infrastructure

Sandbox image definitions, volume layout, warm-pool configuration and the
SEO crawl function (Playwright + Lighthouse).

Egress from sandboxes is deny-by-default (SPEC §9): allow only package
registries, `aigw`, `gitd`, `mcp`, and the project's own app database host.
