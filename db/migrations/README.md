# Control plane migrations

`goose` SQL migrations for the control plane database only. Generated apps'
databases are provisioned separately (SPEC §3.3).

```bash
make db-validate   # parse every migration, no database needed
make db-migrate    # apply pending migrations
make db-status     # what has been applied
make db-setup      # migrate, then grant halyard_app a local login
make db-reset      # drop and recreate the local database (local only)
```

The runner is `packages/db/cmd/migrate`, not the `goose` CLI: the CLI imports a
driver for every database goose supports, which put ClickHouse, MySQL, MSSQL,
Vertica and YDB in the dependency graph of a Postgres-only control plane. See
`docs/verified.md`.

## Rules

**Forward-only and additive** (SPEC §0 rule 6). No migration here has a rollback
section, and `make db-validate` fails the build if one appears. The enforcement
is deliberately structural rather than documentary: a rollback that drops a
table is one command away from deleting a tenant's history. Local iteration uses
`make db-reset`.

**Anything touching `ledger_entries` requires a review gate** (SPEC §19.2). Flag
it in the PR body.

**Row-level security on every tenant table.** Not only those §6 gives an `org_id`
— see below.

## Two deviations from SPEC §6, both deliberate

§6 opens with "Not exhaustive — add columns as needed, but do not rename these
or change their semantics without updating this document." Neither of these
renames anything or changes a meaning.

### `org_id` is denormalised onto every tenant-scoped table

§6 puts `org_id` on ten tables and says to enable RLS on those. Sixteen more
tables reach the org through `projects`, including `secrets`. A defence-in-depth
control that skips the most sensitive table in the schema is not one.

Reaching through the parent instead was measured and rejected: on 100k rows a
`project_id in (select ...)` policy costs 2.71 ms against 0.36 ms for a direct
comparison, because it scans the table and discards non-matching rows rather
than using an index to find them. Its cost tracks the whole table, not the
tenant.

The integrity hole this would normally open — a row whose `org_id` disagrees
with its parent's is invisible to its real owner and visible to someone else —
is closed by a **composite foreign key** onto the parent's `(id, org_id)`. That
is why `projects`, `branches`, `sessions`, `repos`, `ad_accounts`, `deployments`,
`connectors`, `holds` and `github_installations` each carry an
otherwise-redundant `unique (id, org_id)`. A mismatched row is refused by the
database, not by review.

### `templates` and `template_versions` exist

§6 declares `projects.template_version_id references template_versions(id)` but
never defines the table, so `projects` could not be created at all. The shape
comes from `packages/schema/api.openapi.yaml`, which models both. Recorded as
`docs/open-questions.md` Q2.

## Missing foreign keys that §6 implies

Four columns in §6 are bare `uuid`/`bigint` where every neighbour is
constrained. The constraints are added, because a pointer to a deleted row is
the kind of dangling reference that renders as half a page:

| Column                           | Now references             | Added in |
| -------------------------------- | -------------------------- | -------- |
| `branches.preview_deployment_id` | `deployments (id, org_id)` | `00007`  |
| `turns.hold_id`                  | `holds (id, org_id)`       | `00011`  |
| `usage_events.ledger_entry_id`   | `ledger_entries (id)`      | `00011`  |
| `rank_snapshots.project_id`      | `projects (id, org_id)`    | `00010`  |

## Tables with no policy, and why

- **`users`** — a global identity that may belong to many orgs (§8), and sign-in
  must find a user by email _before_ any org is known, so an org-scoped policy
  would make authentication impossible. The consequence is stated rather than
  mitigated: a query bug here can read an email outside the caller's orgs. The
  application only reaches users through `org_members`.
- **`templates`, `template_versions`, `capabilities`, `capability_versions`,
  `price_books`** — a global catalogue, identical for every tenant. `halyard_app`
  has `SELECT` only, which is what stops a request editing a price book.

`tests/integration/test_tenancy.py` derives the list of protected tables from
the catalog rather than hard-coding it, so a table added by a later migration
with a grant and no policy **fails a test** instead of quietly becoming
readable across tenants. The exclusions above live in one set in that file, so
adding to them is a visible decision in review.

## The two roles

Migrations run as the owner. Services connect as `halyard_app`.

That is not tidiness — it is the isolation control. A table's owner bypasses its
own policies, and a superuser or `BYPASSRLS` role bypasses them even when the
table is `FORCE`d. Verified: as `halyard` (which compose creates
`rolsuper=t, rolbypassrls=t`) a scoped query returns every tenant's rows with no
error. `db.Open` refuses to start against such a role for exactly this reason.
