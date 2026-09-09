# Provenance — `frontend-design`

Vendored, unmodified, from Anthropic's public skills repository.

| | |
| --- | --- |
| Source | https://github.com/anthropics/skills/tree/main/skills/frontend-design |
| Pinned commit | `41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f` (2026-09-03, "Update frontend-design skill to avoid generic design defaults (#1713)") |
| Vendored on | 2026-09-09 |
| Licence | Apache License 2.0 — full text in [LICENSE.txt](LICENSE.txt) |

## Integrity

Verified byte-identical to upstream at the pinned commit. Git blob hashes match
the values GitHub reports for those paths:

| File | Blob SHA-1 |
| --- | --- |
| `SKILL.md` | `a5333457c414d20d625f307df945842c0952ecc3` |
| `LICENSE.txt` | `f433b1a53f5b830a205fd2df78e2b34974656c7b` |

Re-check at any time:

```bash
git hash-object .claude/skills/frontend-design/SKILL.md
```

## Licence compliance

Apache 2.0 obligations, and how each is met:

- **§4(a) retain the licence** — `LICENSE.txt` is vendored alongside the skill.
- **§4(b) state changes** — there are none. Both files are byte-identical to
  upstream, which is why project-specific guidance lives in
  [`CLAUDE.md`](../../../CLAUDE.md) and in this file rather than as edits to
  `SKILL.md`. Keep it that way: it makes updating a clean re-download and diff.
- **§4(d) NOTICE** — upstream ships no `NOTICE` file. The repository's
  `THIRD_PARTY_NOTICES.md` covers bundled fonts and libraries used by other
  skills (BSD-2, GPL-3.0, HPND, OFL-1.1) and does not reference
  `frontend-design`, which is prose only and bundles no third-party assets. No
  notice obligation carries over.

`.prettierignore` excludes `.claude/skills/`, so `make fmt` cannot silently
reformat vendored content and invalidate the hashes above.

## Updating

```bash
curl -sL -H 'Accept: application/vnd.github+json' \
  'https://api.github.com/repos/anthropics/skills/contents/skills/frontend-design/SKILL.md' \
  | python3 -c 'import base64,json,sys; sys.stdout.write(base64.b64decode(json.load(sys.stdin)["content"]).decode())' \
  > /tmp/SKILL.md
diff -u .claude/skills/frontend-design/SKILL.md /tmp/SKILL.md
```

Review the diff, then update the pinned commit and both blob hashes in this
file in the same change. Branch as `update/frontend-design-skill`.
