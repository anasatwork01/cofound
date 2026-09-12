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

/**
 * Where you are, in the mockup's crumb voice: `text-sm` muted beside a wordmark
 * that is now `text-lg` at 700. The two sizes are two steps apart on a
 * seven-step scale, which is the whole hierarchy — there is no rule, no plane
 * and no shadow between them, because the warmer ramp spans about five points
 * of lightness and cannot carry a hierarchy by luminance any more.
 */
function Context({
  org,
  project,
}: {
  readonly org?: string | undefined
  readonly project?: string | undefined
}) {
  if (org === undefined && project === undefined) return null
  return (
    <p className="flex min-w-0 items-baseline gap-2 text-sm">
      {org !== undefined ? <span className="truncate text-ink-muted">{org}</span> : null}
      {org !== undefined && project !== undefined ? (
        <span aria-hidden="true" className="text-ink-muted">
          /
        </span>
      ) : null}
      {project !== undefined ? (
        <span className="truncate font-medium text-ink">{project}</span>
      ) : null}
    </p>
  )
}

export function TopBar({ credits = UNKNOWN_CREDITS, org, project, contextSlot }: TopBarProps) {
  return (
    /*
      52px and fixed, which is the approved mockup's own figure for this bar.

      FIXED, not `min-h` with padding: this is persistent chrome (SPEC §18), and
      a bar that grew when the credit gauge went low would shove every screen
      down by the height of a badge at the exact moment the user is reading one.
      The height therefore has to clear the gauge's tallest state — two rows plus
      the row gap, 45px with the "Running low" chip in the second — which is why
      `status.tsx` keeps that chip at 20px rather than stepping it up with the
      rest of the register.

      The horizontal rhythm is where the room went: space-6 between the identity
      group and the gauge, against space-4 before.
    */
    <header className="flex h-13 items-center gap-6 border-b border-border bg-surface-raised px-4">
      {/*
        Identity and place, baseline-aligned as one group so the crumb sits on
        the wordmark's baseline rather than on its centre — the mockup's top bar,
        exactly. The bar itself stays centre-aligned, for the gauge.

        `/` redirects to your last project or to `/new` (SPEC §18), so the mark
        is a link home rather than a decoration. Plain wordmark: the approved
        mockup owns the identity, and a logo invented here would be a thing to
        throw away.
      */}
      <div className="flex min-w-0 items-baseline gap-3">
        <a
          href="/"
          /* `shrink-0`: when the bar runs out of room the place truncates, never
             the product's own name. */
          className={`shrink-0 text-lg font-bold tracking-tight text-ink ${FOCUS_RING}`}
        >
          Halyard
        </a>
        {contextSlot ?? <Context org={org} project={project} />}
      </div>
      <div className="ml-auto">
        <CreditGauge build={credits.build} runtime={credits.runtime} />
      </div>
    </header>
  )
}
