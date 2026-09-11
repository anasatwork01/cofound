import type { ReactNode } from "react"
import { CreditGauge } from "../credit-gauge/credit-gauge"
import { UNKNOWN_CREDITS, type MeterReading } from "../credit-gauge/credits"

/**
 * The console's persistent chrome, mounted once by the root layout and present
 * on every screen (SPEC §18).
 *
 * It carries three things and no more: the product mark, which org and project
 * you are in, and the credit gauge. The per-project navigation belongs to the
 * project layout, not here — it does not exist on `/new` or `/settings/*`.
 */
export interface TopBarProps {
  /**
   * Both meters default to unknown, which is what the phase 0 console can
   * honestly say: there is no credits endpoint until phase 4.
   */
  readonly credits?: {
    readonly build: MeterReading
    readonly runtime: MeterReading
  }
  readonly org?: string | undefined
  readonly project?: string | undefined
  /**
   * The seam for the org/project switcher.
   *
   * There is no API to list a user's orgs or projects yet, so there is no
   * switcher — inventing one would mean inventing its endpoint. Until then
   * the context reads as text. When the API lands, the switcher mounts here
   * as a client component: a slot rather than a callback prop, because the
   * root layout that renders this bar is a Server Component and cannot pass a
   * function across the boundary.
   */
  readonly contextSlot?: ReactNode
}

/**
 * The focus ring, drawn from `--color-focus`. SPEC §18 requires a visible one
 * throughout; `focus-visible` rather than `focus` so a mouse click does not
 * leave a ring behind.
 *
 * The token layer declares the same ring globally on `:focus-visible`, so this
 * is deliberate redundancy rather than the only copy: the chrome keeps its ring
 * even if it is ever rendered without that stylesheet, and a test can assert it
 * without parsing CSS. Kept module-local so it does not become a third place
 * the ring is defined.
 */
const FOCUS_RING = "rounded-sm outline-focus focus-visible:outline-2 focus-visible:outline-offset-2"

function Context({
  org,
  project,
}: {
  readonly org?: string | undefined
  readonly project?: string | undefined
}) {
  if (org === undefined && project === undefined) return null
  return (
    <p className="flex min-w-0 items-center gap-2 text-sm">
      {org !== undefined ? <span className="truncate text-ink-muted">{org}</span> : null}
      {org !== undefined && project !== undefined ? (
        <span aria-hidden="true" className="text-ink-muted">
          /
        </span>
      ) : null}
      {project !== undefined ? <span className="truncate text-ink">{project}</span> : null}
    </p>
  )
}

export function TopBar({ credits = UNKNOWN_CREDITS, org, project, contextSlot }: TopBarProps) {
  return (
    <header className="flex h-14 items-center gap-4 border-b border-border bg-surface-raised px-4">
      {/*
        `/` redirects to your last project or to `/new` (SPEC §18), so the mark
        is a link home rather than a decoration. Plain wordmark: the approved
        mockup owns the identity, and a logo invented here would be a thing to
        throw away.
      */}
      <a href="/" className={`text-sm font-semibold tracking-tight text-ink ${FOCUS_RING}`}>
        Halyard
      </a>
      {contextSlot ?? <Context org={org} project={project} />}
      <div className="ml-auto">
        <CreditGauge build={credits.build} runtime={credits.runtime} />
      </div>
    </header>
  )
}
