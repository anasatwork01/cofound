"use client"

import { useEffect } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ErrorNotice } from "@/components/error-notice"
import { PageHeader } from "@/components/page-header"
import { routes } from "@/lib/routes"
import { useSession, type SessionState } from "@/lib/session"

/**
 * Where `/` sends you, as a pure function of who you are.
 *
 * SPEC §18: "`/` → redirect to last project or /new". Two of those three words
 * are already true; "last project" is the part that is not, and the reason is
 * unchanged from task 0.11: nothing in `api.openapi.yaml` records which project
 * a user had open, and inventing an endpoint for it is what CLAUDE.md working
 * agreement 4 forbids. `GET /projects` could stand in — most recently created,
 * say — but "last created" is not "last opened", and guessing wrong drops
 * someone into the wrong project on every sign-in. So a signed-in reader goes
 * to `/new`, which now lists their projects: one click from there to any of
 * them, and the right screen for the account that has none.
 *
 * Whoever fills that seam: decide first whether last-opened is stored
 * server-side (a column, and a write on every project open) or client-side
 * (per-device, like `lib/current-org.ts`). Then this returns a project route
 * with `/new` as the fallback.
 *
 * `null` means stay put. Loading is not a destination — moving before the
 * answer arrives is how a signed-in user ends up at the sign-in screen — and
 * neither is a session the console could not reach, where redirecting to sign
 * in would be a guess dressed as a fact.
 */
export function homeDestination(state: SessionState): string | null {
  switch (state.status) {
    case "signed-in":
      // No org means nothing to list, nothing to create a project in, and no
      // screen in SPEC §18's table that works. Making one is the next step.
      return state.session.orgs.length === 0 ? routes.newOrg : routes.newProject
    case "signed-out":
      return routes.signIn
    case "loading":
    case "unreachable":
      return null
  }
}

/**
 * The client half of `/`.
 *
 * It renders the same heading in every state on purpose. The three states are
 * told apart by what is under it, never by the page changing identity, so the
 * common case — a fast session check followed by a redirect — is a beat of
 * quiet rather than a flash of a screen nobody was meant to read.
 */
export function HomeRouter() {
  const state = useSession()
  const router = useRouter()
  const destination = homeDestination(state)

  useEffect(() => {
    // `replace`, not `push`: `/` is a junction, and leaving it in the history
    // means Back from `/new` returns here and bounces forward again.
    if (destination !== null) router.replace(destination)
  }, [destination, router])

  return (
    <div className="mx-auto w-full max-w-md space-y-8 px-5 py-16">
      <PageHeader title="Opening Halyard" />
      {state.status === "unreachable" ? (
        <ErrorNotice
          error={state.error}
          action={{
            label: "Try again",
            onClick: () => {
              void state.query.refetch()
            },
          }}
        >
          <Link href={routes.signIn} className="font-medium text-ink underline">
            Sign in
          </Link>
        </ErrorNotice>
      ) : (
        <p className="text-base text-ink-muted" aria-live="polite">
          Checking who you are.
        </p>
      )}
    </div>
  )
}
