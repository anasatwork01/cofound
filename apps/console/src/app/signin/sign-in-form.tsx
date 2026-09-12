"use client"

import { useId, useState } from "react"
import { useMutation } from "@tanstack/react-query"
import { RequestMagicLinkBodySchema } from "@halyard/schema/zod/api-v1"
import { ErrorNotice } from "@/components/error-notice"
import { GOOGLE_SIGN_IN_URL, requestMagicLink } from "@/lib/api"

/**
 * SPEC §8's two ways in: an emailed link, and Google.
 *
 * ---------------------------------------------------------------------------
 * THE 202 IS A PROPERTY, NOT A SHRUG
 *
 * `POST /auth/magic-link` answers 202 whether or not the address has an
 * account — `api.openapi.yaml` says so, and the handler says why: a different
 * answer for a known address turns this endpoint into a way to test whether a
 * given person has a Halyard account. For a product people build businesses on,
 * that is worth protecting.
 *
 * The screen has to hold the same line. "We have sent you a link" implies the
 * address was recognised; "If you have an account, check your email" implies
 * the opposite and is equally informative if you send it twice. The copy below
 * says what is unconditionally true — a link is on its way to the address you
 * typed — and says nothing about what was found at the other end, because
 * nothing was looked up on the console's behalf.
 *
 * ---------------------------------------------------------------------------
 * RUNNING THIS LOCALLY
 *
 * No email arrives on a laptop. There is no transactional email provider yet
 * (docs/open-questions.md Q5), so `api` runs `LogMailer`, which prints the link
 * instead of sending it — at DEBUG and only at DEBUG, because the chassis
 * redactor strips user content from any record at info or above. So:
 *
 *     LOG_LEVEL=debug go run ./services/api
 *
 * and the line is `msg="magic link"` with the full
 * `http://localhost:3000/auth/callback?token=...` in it. Paste it into the
 * browser. The token is single-use and expires.
 */
export function SignInForm() {
  const [email, setEmail] = useState("")
  const [invalid, setInvalid] = useState<string | null>(null)
  const [sentTo, setSentTo] = useState<string | null>(null)
  const emailId = useId()
  const invalidId = useId()

  const send = useMutation({
    mutationFn: requestMagicLink,
    onSuccess: (_result, body) => {
      setSentTo(body.email)
    },
  })

  if (sentTo !== null) {
    return (
      <section className="rounded-md border border-border bg-surface-raised p-6">
        <h2 className="text-xl font-semibold text-ink">Check your email</h2>
        {/* Says what was done, never what was found. See the note above: any
            sentence here that distinguishes a known address from an unknown one
            gives away exactly what the endpoint's 202 is protecting. */}
        <p className="mt-3 text-base text-ink">
          A sign-in link is on its way to <span className="font-medium">{sentTo}</span>. Open it in
          this browser to finish.
        </p>
        <p className="mt-2 text-sm text-ink-muted">
          The link works once, and only for a few minutes.
        </p>
        <button
          type="button"
          onClick={() => {
            setSentTo(null)
            send.reset()
          }}
          className="mt-5 rounded-sm border border-border-strong px-4 py-2.5 text-sm font-medium text-ink hover:bg-surface-sunken"
        >
          Use a different address
        </button>
      </section>
    )
  }

  return (
    <div className="space-y-6">
      <form
        // The console owns the validation, so the console can say what is wrong
        // in its own voice and attach it to the field. Leaving `required` and
        // `type=email` to the browser means a native bubble fires INSTEAD of
        // submit, the handler never runs, and the message below can never be
        // shown — two validation systems, one of which is unstyleable and
        // announced inconsistently. `type=email` stays for the keyboard it
        // gives a phone.
        noValidate
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          // A submit while one is in flight is the same submit. Ignoring it is
          // what makes an idempotency key unnecessary here (see `lib/api.ts`).
          if (send.isPending) return

          const parsed = RequestMagicLinkBodySchema.safeParse({ email: email.trim() })
          if (!parsed.success) {
            setInvalid(
              email.trim() === ""
                ? "Type the email address you want the link sent to."
                : "That does not look like an email address. Check it and try again.",
            )
            return
          }
          setInvalid(null)
          send.mutate(parsed.data)
        }}
      >
        <div className="space-y-2">
          <label htmlFor={emailId} className="block text-sm font-medium text-ink">
            Email address
          </label>
          <input
            id={emailId}
            name="email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(event) => {
              setEmail(event.target.value)
              if (invalid !== null) setInvalid(null)
            }}
            aria-invalid={invalid === null ? undefined : true}
            aria-describedby={invalid === null ? undefined : invalidId}
            /* The border is the only thing saying "this is a box you type in" —
               field and page are both near-white — so it is the boundary token
               that clears WCAG 2.2 SC 1.4.11's 3:1 rather than the decorative
               one. */
            className="block w-full rounded-sm border border-border-strong bg-surface-raised px-3 py-2.5 text-base text-ink"
          />
          {invalid === null ? null : (
            <p id={invalidId} className="text-sm text-ink">
              {invalid}
            </p>
          )}
        </div>

        <button
          type="submit"
          className="w-full rounded-sm bg-agent px-4 py-2.5 text-base font-medium text-agent-ink"
        >
          {/* The action keeps its name while it is happening (SPEC §18). */}
          {send.isPending ? "Emailing a link" : "Email me a link"}
        </button>
      </form>

      {send.isError ? <ErrorNotice error={send.error} /> : null}

      <div className="flex items-center gap-3" aria-hidden="true">
        <span className="h-px flex-1 bg-border" />
        <span className="text-xs text-ink-muted">or</span>
        <span className="h-px flex-1 bg-border" />
      </div>

      {/*
        A link, not a button, and a full-page navigation rather than a fetch:
        `GET /auth/google/start` answers 303 to Google and sets the CSRF state
        and PKCE verifier cookies on the way, neither of which survives being
        read by `fetch`.

        It is always shown. The endpoint 404s when Google is not configured, and
        nothing in `api.openapi.yaml` reports whether it is — so the console
        cannot know, and hiding the button on a guess would hide it on every
        deployment that does have Google. A reader who lands on that 404 still
        has the email field, which works everywhere.
      */}
      <a
        href={GOOGLE_SIGN_IN_URL}
        className="block w-full rounded-sm border border-border-strong px-4 py-2.5 text-center text-base font-medium text-ink hover:bg-surface-sunken"
      >
        Continue with Google
      </a>
    </div>
  )
}
