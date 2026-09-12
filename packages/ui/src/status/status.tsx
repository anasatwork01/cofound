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
 * silhouette that survives greyscale and any form of colour blindness, and the
 * three are distinguishable at 10px, which a hue shift is not.
 *
 * The mapping is the approved mockup's, not this file's invention — SPEC §3.1,
 * `docs/mockup.html`, the `.pip` block:
 *
 *   violet   circle     the agent owns this
 *   amber    triangle   this is waiting on you
 *   teal     square     this is live
 *
 * It used to be diamond / triangle / disc here, which put a circle on `live`
 * and left `agent` with a shape the mockup never draws — so the badge and the
 * project rail, which teaches the same three marks before they are needed,
 * taught two different alphabets. The square is also the radius rule in
 * miniature: `live` is a fact you read, and facts carry no radius.
 */
const GLYPH: Record<StatusState, ReactNode> = {
  agent: <circle cx="6" cy="6" r="5" />,
  waiting: <path d="M6 1L11 10H1Z" />,
  live: <rect x="1" y="1" width="10" height="10" />,
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
 * The chip itself.
 *
 * Operable-looking, so radius-sm — 8px since the warming, which on a 20px chip
 * reads as a soft capsule rather than as a box.
 *
 * The HEIGHT is deliberately not stepped up with the rest of the register. This
 * badge is chrome as often as it is content: the credit gauge puts one inside
 * the top bar, and a taller chip stops the gauge's two rows fitting the 52px bar
 * the approved mockup fixes. The room went sideways instead — 12px of side
 * padding, and the mockup's own space-2 rhythm between the mark and the word.
 *
 * And no half-step spacing, anywhere the gauge can render this. A half-step
 * lands in the class name, the class name lands in the DOM, and
 * `credit-gauge.test.tsx` reads the rendered HTML for a digit-dot-digit to hold
 * SPEC §16.6's "never display fractions" — so a chip inside the gauge would fail
 * a credits test for a reason that has nothing to do with credits.
 */
const CHIP = "inline-flex items-center gap-2 rounded-sm px-3 py-1 text-xs leading-none font-medium"

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
    <span data-state={state} className={`${CHIP} ${TONE[state]}`}>
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
