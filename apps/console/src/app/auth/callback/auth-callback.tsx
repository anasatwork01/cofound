"use client"

import { useEffect, useRef, type ReactNode } from "react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ErrorNotice } from "@/components/error-notice"
import { queryKeys, verifyMagicLink } from "@/lib/api"
import { routes } from "@/lib/routes"

/**
 * Where a sign-in lands. Both of them.
 *
 * The address is not a choice made here.
 * `services/api/internal/auth/magiclink.go` builds every emailed link as
 * `CONSOLE_ORIGIN + "/auth/callback?token=..."`, and the Google OAuth handler
 * redirects the browser to the same path with either nothing or
 * `?error=<reason>`. Three arrivals, therefore:
 *
 *   ?token=...        an emailed link. Exchange it for a session.
 *   ?error=<reason>   Google refused or was refused. Say which, in our words.
 *   neither           Google succeeded; the cookie is already set. Move on.
 */

/**
 * The console owns this wording, and that is the API's decision rather than a
 * liberty taken here: `redirectToConsole` sends "a stable code, never a
 * message", precisely so an upstream provider's error string never reaches a
 * page. So there is no envelope to surface — the codes below are the whole
 * vocabulary the API emits, and each gets both halves SPEC §18 asks for.
 */
const REASONS: Record<string, { readonly what: string; readonly how: string }> = {
  declined: {
    what: "You did not finish signing in with Google.",
    how: "Start again, or use your email address instead.",
  },
  expired: {
    what: "That sign-in attempt took too long.",
    how: "Start again from the sign-in screen.",
  },
  state_mismatch: {
    what: "That sign-in could not be verified.",
    how: "Start again from the sign-in screen. If it keeps happening, allow cookies for this site.",
  },
  exchange_failed: {
    what: "Google did not complete the sign-in.",
    how: "Start again, or use your email address instead.",
  },
  email_unverified: {
    what: "Google has not verified that email address.",
    how: "Verify it with Google, or sign in with an emailed link instead.",
  },
}

const UNKNOWN_REASON = {
  what: "That sign-in did not complete.",
  how: "Start again from the sign-in screen.",
}

function Panel({ children }: { readonly children: ReactNode }) {
  return (
    <section className="rounded-md border border-border bg-surface-raised p-6">{children}</section>
  )
}

function BackToSignIn({ label }: { readonly label: string }) {
  return (
    <Link
      href={routes.signIn}
      className="mt-5 inline-block rounded-sm border border-border-strong px-4 py-2.5 text-sm font-medium text-ink hover:bg-surface-sunken"
    >
      {label}
    </Link>
  )
}

export function AuthCallback() {
  const params = useSearchParams()
  const router = useRouter()
  const queryClient = useQueryClient()

  const token = params.get("token")
  const reason = params.get("error")

  const verify = useMutation({
    mutationFn: verifyMagicLink,
    onSuccess: (session) => {
      // The response IS the session, so seed it rather than round-tripping for
      // something already in hand; `/` reads this key the moment it mounts.
      queryClient.setQueryData(queryKeys.session, session)
      router.replace(routes.home)
    },
  })

  /**
   * Fired once, by hand, rather than through a query.
   *
   * A link token is single-use: the second exchange of the same token is
   * answered exactly like an unknown one. So this must not be anything that
   * refetches — not on remount, not on focus, not on a reconnect — and it must
   * survive React re-running an effect in development. The ref is the guard;
   * `verify.isPending` is not, because the effect can run again before the
   * mutation has started.
   */
  const fired = useRef(false)
  const mutate = verify.mutate
  useEffect(() => {
    if (fired.current) return
    fired.current = true
    if (token !== null && token !== "") {
      mutate({ token })
      return
    }
    if (reason === null) {
      // Google's success path: the API set the cookie before redirecting here,
      // so there is nothing to exchange and `/` will read the session.
      router.replace(routes.home)
    }
  }, [token, reason, mutate, router])

  if (reason !== null) {
    const said = REASONS[reason] ?? UNKNOWN_REASON
    return (
      <Panel>
        <h2 className="text-xl font-semibold text-ink">{said.what}</h2>
        <p className="mt-3 text-base text-ink">{said.how}</p>
        <BackToSignIn label="Back to sign in" />
      </Panel>
    )
  }

  if (verify.isError) {
    return (
      <ErrorNotice error={verify.error}>
        <BackToSignIn label="Back to sign in" />
      </ErrorNotice>
    )
  }

  return (
    <Panel>
      <h2 className="text-xl font-semibold text-ink">Signing you in</h2>
      <p className="mt-3 text-base text-ink-muted">This takes a moment.</p>
    </Panel>
  )
}
