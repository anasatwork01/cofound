/**
 * Credit arithmetic for the gauge (SPEC §16).
 *
 * Kept apart from the component because the rules here are the specification's,
 * not the layout's: what a reading means when there is no reading, when the
 * runtime ladder starts, and the fact that a credit is never shown as a
 * fraction. Pure functions, tested without a DOM.
 *
 * Nothing in this file is money arithmetic. The ledger is `numeric(14,4)` and
 * settles server-side (SPEC §16.1); these are display numbers, and rounding
 * them here can never move a balance.
 */

/**
 * SPEC §16.3's runtime ladder opens at 80%: "warn at 80% → throttle the AI
 * gateway at 100% → disable AI features but keep the site serving → suspend
 * only after a grace period". Only the first rung is a console concern.
 *
 * Held as a percentage rather than 0.8 so the comparison below can stay in
 * integer arithmetic — `committed * 100 >= allowance * 80` is exact where
 * `committed / allowance >= 0.8` invites a binary-floating-point near-miss at
 * exactly the threshold the spec names.
 */
export const RUNTIME_WARNING_PERCENT = 80

/**
 * What one meter knows about itself.
 *
 * `unknown` is a first-class reading, not a zero. There is no credits endpoint
 * before phase 4 — `packages/schema/api.openapi.yaml` says so in its own header
 * — and a gauge drawn at zero would tell every user of the phase 0 console that
 * they are out of credits. It is not loading either; it is unbuilt.
 */
export type MeterReading =
  | { readonly kind: "unknown" }
  | {
      readonly kind: "measured"
      /** Credits already settled against this meter's allowance. */
      readonly used: number
      /** Credits available for the period. */
      readonly allowance: number
      /** Credits reserved by turns in flight and not yet settled (SPEC §16.2). */
      readonly hold?: number | undefined
    }

/** A reading that has numbers in it. */
export type MeasuredReading = Extract<MeterReading, { kind: "measured" }>

/** The one reading the phase 0 console can honestly produce for both meters. */
export const UNKNOWN_CREDITS = {
  build: { kind: "unknown" },
  runtime: { kind: "unknown" },
} as const satisfies { readonly build: MeterReading; readonly runtime: MeterReading }

/** Everything the bar needs, already rounded. */
export interface MeterDisplay {
  /** Settled credits, rounded (SPEC §16.6). */
  readonly used: number
  /** Held credits, rounded. */
  readonly hold: number
  /** The period's allowance, rounded. */
  readonly allowance: number
  /** `used + hold`, rounded — what the filled part of the bar represents. */
  readonly committed: number
  /** Width of the solid segment, whole percent. */
  readonly usedPercent: number
  /** Width of the hatched segment, whole percent; never pushes the total past 100. */
  readonly holdPercent: number
}

const clamp = (n: number, lo: number, hi: number) => Math.min(Math.max(n, lo), hi)

/** Anything non-finite is a bug upstream; treat it as nothing rather than paint NaN. */
const finite = (n: number) => (Number.isFinite(n) ? n : 0)

/**
 * SPEC §16.6: "Never display fractions." Rounding happens here, once, and
 * every number the gauge renders — text, ARIA value, bar width — comes through
 * this module, so a fraction cannot reach the DOM by another route.
 *
 * `en-US` grouping is hardcoded deliberately: console i18n is SPEC §19.3 work
 * and a locale-dependent separator would make this untestable in the meantime.
 */
export function formatCredits(credits: number): string {
  return Math.round(clamp(finite(credits), 0, Number.MAX_SAFE_INTEGER)).toLocaleString("en-US")
}

/**
 * A segment that exists must be visible. A 0.4% hold rounds to nothing and the
 * hatching disappears, which reads as "no hold" — the opposite of the truth.
 * Give any non-zero amount at least one percent, if there is room for it.
 */
function segmentPercent(amount: number, allowance: number, room: number): number {
  if (allowance <= 0 || amount <= 0 || room <= 0) return 0
  return clamp(Math.max(1, Math.round((amount / allowance) * 100)), 0, room)
}

export function meterDisplay(reading: MeasuredReading): MeterDisplay {
  // Rounded first, then measured. Deriving the geometry from the same numbers
  // the label prints is what stops a bar from showing a hold that the text
  // beside it has rounded away.
  const used = Math.round(Math.max(0, finite(reading.used)))
  const hold = Math.round(Math.max(0, finite(reading.hold ?? 0)))
  const allowance = Math.round(Math.max(0, finite(reading.allowance)))

  const usedPercent = segmentPercent(used, allowance, 100)
  const holdPercent = segmentPercent(hold, allowance, 100 - usedPercent)

  return { used, hold, allowance, committed: used + hold, usedPercent, holdPercent }
}

/**
 * SPEC §16.3's first rung. Holds count: a reserved credit is spent as far as
 * the user's next action is concerned, and warning only after settlement would
 * warn too late.
 *
 * Compared against the exact reading rather than the rounded display value, so
 * the warning appears at the threshold the spec names and not a rounding step
 * either side of it.
 */
export function isRuntimeLow(reading: MeterReading): boolean {
  if (reading.kind === "unknown") return false
  const committed = Math.max(0, finite(reading.used)) + Math.max(0, finite(reading.hold ?? 0))
  const allowance = Math.max(0, finite(reading.allowance))
  if (allowance <= 0) return committed > 0
  return committed * 100 >= allowance * RUNTIME_WARNING_PERCENT
}
