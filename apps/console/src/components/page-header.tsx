import type { ReactNode } from "react"

/**
 * The heading every screen opens with.
 *
 * No eyebrow label above the title: a small capitalised word repeating the
 * section you just clicked is decoration, and the nav already says where you
 * are. The lede is the one line that tells you what this screen is for.
 */
export function PageHeader({
  title,
  lede,
  actions,
}: {
  title: string
  lede?: string
  actions?: ReactNode
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="max-w-prose">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">{title}</h1>
        {lede === undefined ? null : <p className="mt-2 text-sm text-ink-muted">{lede}</p>}
      </div>
      {actions === undefined ? null : <div className="flex items-center gap-2">{actions}</div>}
    </header>
  )
}
