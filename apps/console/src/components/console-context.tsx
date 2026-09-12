"use client"

import { useId } from "react"
import { useRouter } from "next/navigation"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { queryKeys, signOut } from "@/lib/api"
import { useCurrentOrg } from "@/lib/current-org"
import { routes } from "@/lib/routes"
import { useSession } from "@/lib/session"

/**
 * What goes in the top bar's `contextSlot`: which organisation you are acting
 * as, and the way out.
 *
 * `packages/ui/src/chrome/top-bar.tsx` left this slot open and said why — the
 * bar is rendered by a Server Component, which cannot pass a callback across
 * the boundary, so the switcher has to arrive as a node rather than as props.
 * SPEC §8 puts org switching here: "users may belong to many orgs, and the
 * project picker switches between them".
 *
 * Renders NOTHING at all until the session has answered. A chrome element that
 * appeared, said "signed out", and then became an org name would flash the
 * wrong answer on every page load, and the top bar is on every screen.
 *
 * A `<select>` rather than a menu button: it is one choice from a short list,
 * which is what a select is, and it arrives keyboard-operable, screen-reader
 * announced and correct on a touch device without a line of code. A custom
 * listbox would be a week of ARIA to reach the same place.
 */
export function ConsoleContext() {
  const state = useSession()
  const orgs = state.status === "signed-in" ? state.session.orgs : []
  const { current, select } = useCurrentOrg(orgs)
  const selectId = useId()

  const router = useRouter()
  const queryClient = useQueryClient()

  const leave = useMutation({
    mutationFn: signOut,
    // Both halves matter. Clearing the cache stops the next screen rendering
    // the last user's orgs out of it, and it has to happen before the
    // navigation or `/` reads the stale session and sends them back in.
    onSuccess: () => {
      queryClient.clear()
      router.replace(routes.signIn)
    },
    // A sign-out that failed still means the reader wants out. The cookie may
    // survive, so the API is the authority and `/` will work out the truth.
    onError: () => {
      queryClient.clear()
      router.replace(routes.signIn)
    },
  })

  if (state.status !== "signed-in") return null

  return (
    <div className="flex min-w-0 items-baseline gap-3">
      {current === null ? null : orgs.length === 1 ? (
        <span className="truncate text-sm text-ink-muted">{current.name}</span>
      ) : (
        <>
          <label htmlFor={selectId} className="sr-only">
            Organisation
          </label>
          {/* A visible label would cost a line of chrome on every screen for a
              word the control's content already says. The name is still
              programmatic, which is what a screen reader needs and what a
              placeholder would not have given. */}

          <select
            id={selectId}
            value={current.slug}
            onChange={(event) => {
              select(event.target.value)
            }}
            className="max-w-40 truncate rounded-sm border border-border bg-surface-raised px-2 py-1 text-sm text-ink"
          >
            {orgs.map((org) => (
              <option key={org.id} value={org.slug}>
                {org.name}
              </option>
            ))}
          </select>
        </>
      )}
      <button
        type="button"
        onClick={() => {
          if (!leave.isPending) leave.mutate()
        }}
        /* Padded to clear WCAG 2.2 SC 2.5.8's 24x24 CSS pixel floor, which a
           bare text button at `text-sm` misses by about five pixels. The
           browser tier of the axe run measures this; the unit tier cannot,
           because happy-dom lays nothing out. */
        className="rounded-sm px-2 py-1 text-sm text-ink-muted hover:text-ink"
      >
        {/* SPEC §18: an action keeps its name through the whole flow. */}
        {leave.isPending ? "Signing out" : "Sign out"}
      </button>
    </div>
  )
}
