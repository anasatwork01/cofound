"use client"

import { useEffect } from "react"
import { PageHeader } from "@/components/page-header"

/**
 * The route-level error boundary. Client by necessity: Next requires it.
 *
 * SPEC §18: say what happened, say how to fix it, do not apologise. The digest
 * is the one piece of machine vocabulary on the screen, and it is demoted to
 * small monospace the same way a commit SHA is — present for a support
 * conversation, not competing with the sentence that tells you what to do.
 */
export default function ScreenError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    // SEAM: task 0.13 adds Sentry and this becomes a report, not a log line.
    console.error(error)
  }, [error])

  return (
    <div className="mx-auto w-full max-w-4xl space-y-5 px-5 py-8">
      <PageHeader
        title="This screen did not load"
        lede="Nothing you did caused it and nothing was lost. Try it again, and if it keeps happening reload the page."
      />
      {/* `border-strong`, not `border`: the boundary is what says this is a
          control rather than a line of text, and WCAG 2.2 SC 1.4.11 asks 3:1
          of a boundary doing that job. `border` is for dividers and card
          edges, where the content carries the meaning. */}
      <button
        type="button"
        onClick={reset}
        className="rounded-md border border-border-strong px-3 py-2 text-sm font-medium text-ink hover:bg-surface-sunken"
      >
        Try again
      </button>
      {error.digest === undefined ? null : (
        <p className="font-mono text-xs text-ink-muted">{error.digest}</p>
      )}
    </div>
  )
}
