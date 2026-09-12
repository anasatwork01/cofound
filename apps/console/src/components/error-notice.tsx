import type { ReactNode } from "react"
import { ApiError } from "@/lib/api"

/**
 * A failed request, drawn the way SPEC §18 asks: what happened, then what to do
 * about it, and no apology.
 *
 * ---------------------------------------------------------------------------
 * THE API'S OWN WORDS, NOT OURS
 *
 * Both halves come from the error envelope. `common.schema.json` splits `code`,
 * `message` and `fix` precisely so the server can say "You are not signed in."
 * / "Sign in and try again." and the console can put both on the screen without
 * writing a sentence of its own.
 *
 * This component exists because the console did the opposite once and nothing
 * caught it: the template gallery replaced every server message with "The
 * templates did not load." and every server fix with "Check your connection,
 * then try again." — so a clean 401, whose whole content is "sign in", read as
 * a network problem and sent the reader to check their wifi. A generic fix is
 * not a smaller version of the real one; it points somewhere else.
 *
 * The console owns the copy in exactly one case: the request never reached a
 * server, so there is no envelope and no server opinion to surface. That is
 * the only branch below with a literal in it.
 *
 * ---------------------------------------------------------------------------
 * WHY IT IS NOT AMBER
 *
 * SPEC §18 fixes amber as "waiting on you" and requires it to mean the same
 * thing every time: an ads approval, an incomplete Stripe onboarding, a
 * DNS-pending domain. Those are states the product is correctly in, holding
 * still for a decision only the reader can make. A request that failed is none
 * of them. Painting it amber would quietly redefine the state to "waiting on
 * you, or broken", and then the three states stop being one learned pattern.
 *
 * §18 defines no error state, so this one is built from neutral surface
 * tokens: `role="alert"` carries the urgency, the copy carries the meaning, and
 * the boundary is the token that has to be seen so the notice is findable
 * without borrowing a colour that means something else. If a later screen needs
 * error to be a COLOUR, that is a change to §18's vocabulary and belongs in the
 * spec, not in a component.
 */
export function ErrorNotice({
  error,
  action,
  children,
}: {
  readonly error: unknown
  /** A retry, or a way onward. Omitted when there is nothing useful to offer. */
  readonly action?: { readonly label: string; readonly onClick: () => void }
  /** A link the fix names — "sign in", say — which a button cannot be. */
  readonly children?: ReactNode
}) {
  const said = error instanceof ApiError ? error : null

  // The one case the console owns: no response, so no envelope. `fetch` rejects
  // with a TypeError for DNS failure, offline, CORS and a dropped connection
  // alike, and none of them can be told apart from here.
  const what = said?.message ?? "The request did not reach the server."
  const how = said === null ? "Check your connection, then try again." : said.fix

  return (
    <div
      role="alert"
      className="max-w-prose rounded-md border border-border-strong bg-surface-raised p-5"
    >
      <p className="text-base font-medium text-ink">{what}</p>
      {/* An envelope may carry no `fix`. Nothing is invented to fill the gap —
          a made-up next step is the failure this component exists to prevent. */}
      {how === undefined ? null : <p className="mt-2 text-base text-ink">{how}</p>}
      {children === undefined ? null : <div className="mt-3 text-sm">{children}</div>}
      {action === undefined ? null : (
        <button
          type="button"
          onClick={action.onClick}
          className="mt-4 rounded-sm border border-border-strong bg-surface-raised px-4 py-2.5 text-sm font-medium text-ink hover:bg-surface-sunken"
        >
          {action.label}
        </button>
      )}
      {/* Present for a support conversation, demoted so it is never the first
          thing read. SPEC §18: SHAs and ids in small monospace. */}
      {said?.requestId === undefined ? null : (
        <p className="mt-4 font-mono text-xs text-ink-muted">{said.requestId}</p>
      )}
    </div>
  )
}
