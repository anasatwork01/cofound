"use client"

/**
 * Which organisation the console is currently acting as.
 *
 * ---------------------------------------------------------------------------
 * WHERE IT LIVES, AND WHY THERE
 *
 * In `localStorage`, on the device, under one key. Nowhere else.
 *
 * It is NOT on the server, because nothing in `api.openapi.yaml` records a
 * user's current org and inventing an endpoint for it is what CLAUDE.md
 * working agreement 4 forbids. It is NOT in a cookie, because the API is on
 * another origin and a cookie set here would never be sent there — the org
 * travels as the `X-Halyard-Org` header on each request instead
 * (`api.openapi.yaml`'s `OrgContext`). It is NOT in the URL, because SPEC §18's
 * route table has no org segment and adding one would change every link in the
 * product. It is NOT in a React context, because a context is lost on reload
 * and the reader would land back in a different org than the one they left.
 *
 * The consequence is honest and worth stating: this is a per-device
 * preference. Switching org on your laptop does not switch it on your phone.
 * That is the correct behaviour for a preference nobody asked the server to
 * remember, and it is the only kind available without inventing an endpoint.
 *
 * The stored value is a HINT, never an authority. It is only ever used to pick
 * one of the memberships the session already returned, so a slug left behind by
 * an org the user was removed from resolves to nothing and the first membership
 * is used instead. A stale or hand-edited value cannot name an org the API
 * would not have let you see anyway — every request is still authorised
 * server-side against the caller's memberships (SPEC §8).
 */
import { useCallback, useSyncExternalStore } from "react"
import type { AuthOrgMembership } from "@/lib/api"

/** Namespaced, so this cannot collide with anything else on the origin. */
export const CURRENT_ORG_KEY = "halyard.current-org"

/**
 * Which membership a stored preference resolves to.
 *
 * Pure, and the whole of the decision: the remembered slug if the user is still
 * in that org, otherwise the first membership, otherwise none. Exported so the
 * rule can be tested without a browser.
 */
export function resolveCurrentOrg(
  orgs: readonly AuthOrgMembership[],
  remembered: string | null,
): AuthOrgMembership | null {
  const named = orgs.find((org) => org.slug === remembered)
  if (named !== undefined) return named
  return orgs[0] ?? null
}

const listeners = new Set<() => void>()

function readStored(): string | null {
  try {
    return window.localStorage.getItem(CURRENT_ORG_KEY)
  } catch {
    // Safari's private mode and a user who blocked storage both throw here
    // rather than returning null. Neither is a reason to fail to render.
    return null
  }
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange)
  // A second tab switching org fires `storage` here but not in the tab that
  // wrote it, which is why the write below notifies local listeners itself.
  window.addEventListener("storage", onChange)
  return () => {
    listeners.delete(onChange)
    window.removeEventListener("storage", onChange)
  }
}

/** Remember an org for next time. Safe to call with a slug that later vanishes. */
export function rememberOrg(slug: string): void {
  try {
    window.localStorage.setItem(CURRENT_ORG_KEY, slug)
  } catch {
    // Storage refused. The switch still takes effect for this page — the
    // listeners below re-render either way — it just will not survive a reload.
  }
  for (const listener of [...listeners]) listener()
}

/**
 * The current org, chosen from the memberships the caller already has.
 *
 * Takes the list rather than reading the session itself, so a component that
 * has not loaded a session yet passes `[]` and gets `null` instead of a
 * second, competing request for the same data.
 *
 * `getServerSnapshot` returns null because there is no `localStorage` during
 * prerender: the server has no idea which org this device prefers, and
 * rendering a guess would hydrate into a mismatch.
 */
export function useCurrentOrg(orgs: readonly AuthOrgMembership[]): {
  readonly current: AuthOrgMembership | null
  readonly select: (slug: string) => void
} {
  const remembered = useSyncExternalStore(subscribe, readStored, () => null)
  const select = useCallback((slug: string) => {
    rememberOrg(slug)
  }, [])
  return { current: resolveCurrentOrg(orgs, remembered), select }
}
