import { redirect } from "next/navigation"
import { routes } from "@/lib/routes"

/**
 * SPEC §18: "`/` → redirect to last project or /new".
 *
 * SEAM: there is no "last project" yet. Nothing in `api.openapi.yaml` records
 * which project a user had open, and inventing an endpoint for it is exactly
 * what CLAUDE.md working agreement 4 forbids. `GET /projects` exists and could
 * stand in — most recently created, say — but "last created" is not "last
 * opened", and guessing wrong sends someone into the wrong project on every
 * sign-in. So this goes to /new, which is also the right answer for the
 * account that has no projects at all.
 *
 * Whoever adds it: decide first whether last-opened is stored server-side (a
 * column, and a write on every project open) or client-side (localStorage, and
 * therefore per-device). Then this becomes a redirect with a fallback, not a
 * fixed one.
 */
export default function HomePage(): never {
  redirect(routes.newProject)
}
