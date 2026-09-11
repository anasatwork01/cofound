"use client"

import {
  selectShowLeaseBanner,
  selectTakeoverPending,
  useBuilderStore,
} from "@/store/builder-store"

/**
 * SPEC §18's write lease, made visible.
 *
 * "When the agent holds it, the editor is read-only with a visible banner and
 * a 'take over' button that requests the lease for the end of the current
 * turn. Never a silently rejecting editor." The banner and the read-only mode
 * come from one pair of selectors over one field, so they cannot disagree:
 * there is no state in which the editor refuses a keystroke and the screen
 * says nothing about why.
 *
 * The button keeps its name through the flow — Take over, Taking over, then
 * gone, because once the lease is yours there is nothing left to say.
 *
 * Colour is not carrying the meaning: the sentence names the agent, and the
 * violet tint only agrees with it. SEAM: `packages/ui` exports a `<Status>`
 * badge for exactly this vocabulary; once the package has an entry point to
 * import it from, the tint and the sentence become
 * `<Status state="agent">The agent is editing</Status>` and this file stops
 * naming a state at all.
 *
 * ---------------------------------------------------------------------------
 * WHY THE REGION IS ALWAYS MOUNTED
 *
 * `role="status"` is only worth having if it announces, and a polite live
 * region announces content added to a region assistive technology was ALREADY
 * observing. A region mounted together with its sentence is not reliably
 * announced at all — so the region here is unconditional and only its contents
 * are conditional. The person who most needs telling that the editor just went
 * read-only is exactly the person a mount-with-text banner tells nothing.
 *
 * The root layout's toast region and `packages/ui`'s credit gauge are built
 * the same way, for the same reason.
 *
 * Empty, the region is `sr-only` rather than `hidden`: `hidden` would take it
 * out of the accessibility tree, which is the one thing that must not happen,
 * and a zero-height block would still collect a `space-y-6` gap from the
 * project layout and open a hole between the header and the editor on every
 * screen where the lease is yours. `sr-only` is out of flow, so it costs no
 * layout and keeps the region observable.
 */
export function LeaseBanner() {
  const show = useBuilderStore(selectShowLeaseBanner)
  const pending = useBuilderStore(selectTakeoverPending)
  const requestTakeover = useBuilderStore((state) => state.requestTakeover)

  return (
    <div role="status" aria-live="polite" className={show ? undefined : "sr-only"}>
      {show ? (
        <div className="flex flex-wrap items-center gap-3 rounded-md border border-agent bg-agent-soft p-3">
          <p className="min-w-0 flex-1 text-sm text-ink">
            {pending
              ? "The agent is finishing this turn. The editor comes back to you as soon as it does."
              : "The agent is editing this project, so the editor is read-only. Take it over and you get it back at the end of this turn."}
          </p>
          <button
            type="button"
            onClick={requestTakeover}
            disabled={pending}
            className="rounded-md bg-agent px-3 py-2 text-sm font-medium text-agent-ink disabled:opacity-60"
          >
            {pending ? "Taking over" : "Take over"}
          </button>
        </div>
      ) : null}
    </div>
  )
}
