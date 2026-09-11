import type { ReactNode } from "react"

/**
 * The three states, and the only three states (SPEC §18):
 *
 *   violet  = agent-owned action
 *   amber   = waiting on you
 *   teal    = live
 *
 * "Used identically for the ads approval, incomplete Stripe onboarding, and a
 * DNS-pending domain — one learned pattern, not three." That sentence is the
 * reason this is a union and not a string, and the reason `<Status>` takes no
 * `color`, `className` or `style`: a fourth state has to be unrepresentable,
 * or the pattern stops being learnable the first time someone needs red.
 *
 * If a situation genuinely does not fit one of the three, that is a
 * specification question for SPEC §18, not a prop.
 */
export type StatusState = "agent" | "waiting" | "live"

export interface StatusProps {
  readonly state: StatusState
  /**
   * The situation, in the user's words: "Approve the ad budget", "Waiting for
   * DNS". Omit it and the badge shows the state itself, which is what a bare
   * "Live" chip wants.
   *
   * Pass the situation, not the state word — the state word is added for
   * assistive technology either way, so `<Status state="live">Live</Status>`
   * announces "Live: Live".
   */
  readonly children?: ReactNode
}

/**
 * Announced before the caller's label, and shown on its own when there is no
 * label. SPEC §18's own vocabulary, kept verbatim so the console and the
 * specification cannot drift apart.
 */
const STATE_WORD: Record<StatusState, string> = {
  agent: "Agent",
  waiting: "Waiting on you",
  live: "Live",
}

/**
 * Colour is never the only carrier (SPEC §18 accessibility). Each state has a
 * silhouette that survives greyscale and any form of colour blindness:
 * diamond, triangle, disc. They are distinguishable at 10px, which a hue shift
 * is not.
 */
const GLYPH: Record<StatusState, ReactNode> = {
  agent: <path d="M6 1L11 6L6 11L1 6Z" />,
  waiting: <path d="M6 1L11 10H1Z" />,
  live: <circle cx="6" cy="6" r="5" />,
}

/**
 * Written out in full rather than composed from the state name, because
 * Tailwind reads these class names out of the source file and cannot see
 * through a template literal.
 *
 * Full-strength background with its own `-ink` on top: that is the pair the
 * token contract guarantees at 4.5:1, which SPEC §18 requires. The `-soft`
 * tints are for surfaces that carry no text.
 */
const TONE: Record<StatusState, string> = {
  agent: "bg-agent text-agent-ink",
  waiting: "bg-waiting text-waiting-ink",
  live: "bg-live text-live-ink",
}

/**
 * One badge, three states, no escape hatch.
 *
 * Renders as a plain inline element with no ARIA role: it is a label, not a
 * live region. A status that *changes* belongs inside a region the caller marks
 * `aria-live`, so the announcement carries the surrounding sentence rather than
 * two words of context-free colour.
 */
export function Status({ state, children }: StatusProps) {
  const word = STATE_WORD[state]
  return (
    <span
      data-state={state}
      className={`inline-flex items-center gap-1 rounded-sm px-2 py-1 text-xs leading-none font-medium ${TONE[state]}`}
    >
      <svg
        width="10"
        height="10"
        viewBox="0 0 12 12"
        fill="currentColor"
        aria-hidden="true"
        focusable="false"
        className="shrink-0"
      >
        {GLYPH[state]}
      </svg>
      {children === undefined ? (
        word
      ) : (
        <>
          <span className="sr-only">{word}: </span>
          {children}
        </>
      )}
    </span>
  )
}
