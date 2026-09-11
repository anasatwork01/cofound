"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { isCurrent, type NavItem } from "@/lib/routes"

/**
 * The horizontal nav used by both the project shell and settings.
 *
 * A client component only because it needs the current pathname to mark the
 * current tab. `aria-current="page"` is what carries that to a screen reader;
 * the weight change and the rule under the label carry it to everyone else.
 * Colour is not doing the work on its own.
 */
export function TabNav({ label, items }: { label: string; items: readonly NavItem[] }) {
  const pathname = usePathname() ?? ""

  return (
    <nav aria-label={label} className="border-b border-border">
      <ul className="-mb-px flex gap-1 overflow-x-auto">
        {items.map((item) => {
          const current = isCurrent(pathname, item.href, items)
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                {...(current ? { "aria-current": "page" as const } : {})}
                className={
                  current
                    ? "inline-block border-b-2 border-ink px-3 py-2 text-sm font-medium text-ink"
                    : "inline-block border-b-2 border-transparent px-3 py-2 text-sm text-ink-muted hover:border-border hover:text-ink"
                }
              >
                {item.label}
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
