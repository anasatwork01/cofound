import Link from "next/link"

/**
 * SPEC §18: "Empty states are invitations to act."
 *
 * So: a title that names what is not here yet, a line that says what will fill
 * it and what you do to make that happen, and — where there is somewhere real
 * to go next — a link that goes there. Never "No data", never a spinner that
 * resolves to nothing, never a "TODO".
 *
 * Deliberately left-aligned inside a hairline panel rather than centred in a
 * shadowed card: an empty screen is a normal state of this product, not an
 * error to be apologised for with a big illustration.
 */
export type EmptyStateAction = {
  readonly href: string
  /** The screen or action's own name, unchanged from wherever else it appears. */
  readonly label: string
}

export function EmptyState({
  title,
  body,
  action,
}: {
  title: string
  body: string
  action?: EmptyStateAction
}) {
  return (
    <section className="max-w-prose rounded-lg border border-border bg-surface-raised p-6">
      <h2 className="text-base font-medium text-ink">{title}</h2>
      <p className="mt-2 text-sm text-ink-muted">{body}</p>
      {action === undefined ? null : (
        /* The panel keeps the hairline `border`, which is decoration around
           content that carries its own meaning. The action does not: its
           boundary is what says "press this" rather than "read this", so it
           takes `border-strong` and WCAG 2.2 SC 1.4.11's 3:1 with it. */
        <Link
          href={action.href}
          className="mt-4 inline-block rounded-md border border-border-strong px-3 py-2 text-sm font-medium text-ink hover:bg-surface-sunken"
        >
          {action.label}
        </Link>
      )}
    </section>
  )
}
