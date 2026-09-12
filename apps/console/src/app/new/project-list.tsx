"use client"

import type { ReactNode } from "react"
import Link from "next/link"
import { useQuery } from "@tanstack/react-query"
import { EmptyState } from "@/components/empty-state"
import { ErrorNotice } from "@/components/error-notice"
import { fetchProjects, queryKeys, type Project } from "@/lib/api"
import { useCurrentOrg } from "@/lib/current-org"
import { routes } from "@/lib/routes"
import { useSession } from "@/lib/session"

/**
 * `GET /v1/projects` — the list phase 0's acceptance criterion ends on.
 *
 * It lives on `/new` rather than at a route of its own. SPEC §18's table has no
 * `/projects`, and this is not a screen missing from it: `/new` is already
 * where you go to start something, and what you have already started belongs
 * next to it — one click to open one, one box to start the next. A `/projects`
 * route would be a second home screen for the same job.
 *
 * The call needs `X-Halyard-Org`, because a project slug is unique only within
 * an org (SPEC §6) and the API refuses to guess: without the header it answers
 * "That request needs an organisation." So the org comes from
 * `lib/current-org.ts`, chosen out of the memberships the session already
 * returned, and it is part of the query key — two orgs, two lists, and a switch
 * must never show the previous org's projects.
 */
export function ProjectList() {
  const state = useSession()
  const orgs = state.status === "signed-in" ? state.session.orgs : []
  const { current } = useCurrentOrg(orgs)

  const projects = useQuery({
    queryKey: queryKeys.projects(current?.slug ?? ""),
    queryFn: () => fetchProjects(current?.slug ?? ""),
    // Not "disabled while loading" but "there is nothing to ask yet": with no
    // org there is no header to send, and the request would be refused.
    enabled: current !== null,
  })

  // Signed out, or the session has not answered. The prompt box above already
  // says which and what to do about it; a second copy of the same sentence is
  // noise, and a projects heading over an empty box implies you have none.
  if (state.status !== "signed-in") return null

  if (current === null) {
    return (
      <Section>
        <EmptyState
          title="No organisation yet"
          body="Projects, credits and teammates all belong to an organisation. Make one and this fills up."
          action={{ href: routes.newOrg, label: "Create an organisation" }}
        />
      </Section>
    )
  }

  if (projects.isPending) {
    return (
      <Section busy>
        <p className="sr-only">Loading your projects</p>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((index) => (
            <div key={index} className="h-24 rounded-md border border-border bg-surface-sunken" />
          ))}
        </div>
      </Section>
    )
  }

  if (projects.isError) {
    return (
      <Section>
        <ErrorNotice
          error={projects.error}
          action={{
            label: "Try again",
            onClick: () => {
              void projects.refetch()
            },
          }}
        />
      </Section>
    )
  }

  const items = [...projects.data.projects].sort(newestFirst)

  if (items.length === 0) {
    return (
      <Section>
        {/* SPEC §18: "Empty states are invitations to act." The invitation is
            the box above, so this points at it rather than repeating it as a
            second button that does the same thing. */}
        <EmptyState
          title="No projects yet"
          body="Describe what you want built in the box above, and the first one appears here while the agent works on it."
        />
      </Section>
    )
  }

  return (
    <Section>
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((project) => (
          <li key={project.id}>
            {/* The whole card is the link: a card with a small link inside it
                gives a keyboard or touch reader a target a fraction of the size
                of the thing they can see. */}
            <Link
              href={routes.builder(project.slug)}
              className="block h-full rounded-md border border-border bg-surface-raised p-5 hover:border-border-strong"
            >
              <span className="block text-base font-medium text-ink">{project.name}</span>
              {/* A slug is an address, not prose, so it is measured type —
                  and SPEC §18 demotes it rather than hiding it. */}
              <span className="mt-2 block font-mono text-xs text-ink-muted">{project.slug}</span>
            </Link>
          </li>
        ))}
      </ul>
    </Section>
  )
}

function Section({
  children,
  busy = false,
}: {
  readonly children: ReactNode
  readonly busy?: boolean
}) {
  return (
    <section className="space-y-4" aria-busy={busy ? true : undefined}>
      <h2 className="text-lg font-semibold text-ink">Your projects</h2>
      {children}
    </section>
  )
}

/**
 * Newest first, by creation. Not "last opened" — nothing records that (see
 * `app/home-router.tsx`) — and not alphabetical, because the thing you started
 * five minutes ago is the thing you came back for.
 */
function newestFirst(a: Project, b: Project): number {
  const difference = Date.parse(b.created_at) - Date.parse(a.created_at)
  if (Number.isNaN(difference) || difference === 0) return a.name.localeCompare(b.name)
  return difference
}
