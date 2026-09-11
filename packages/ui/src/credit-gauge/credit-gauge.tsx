import type { CSSProperties } from "react"
import { Status, type StatusState } from "../status/status"
import { type MeterReading, formatCredits, isRuntimeLow, meterDisplay } from "./credits"

/**
 * "The credit gauge is in the top bar on every screen, showing build and
 * runtime as separate bars with the active hold drawn as hatching. This is
 * deliberate: users need to see burn while causing it." — SPEC §18.
 *
 * Everything else in this console is quiet. This is the element that is
 * allowed presence, because it is the one telling you that an agent is
 * spending your money right now.
 *
 * The two bars are deliberately not one meter split in two. SPEC §16.3 gives
 * them different exhaustion behaviours — build runs out and the builder
 * pauses, harmlessly, while runtime runs out and a paying customer's site
 * starts shedding features — so they are labelled separately, stacked rather
 * than joined, and carry different state colours.
 *
 * Colour reuses SPEC §18's three states rather than inventing meter colours:
 *
 *   build   → agent   the builder spends it, on your behalf, while you watch
 *   runtime → live    your live site spends it, serving real end users
 *   runtime → waiting once §16.3's 80% rung is reached, because topping up is
 *                     your action, which is exactly what amber means
 *
 * No state, no effects, no handlers: this renders on the server. The polling
 * client that feeds it real numbers arrives with the credits API in task 4.9.
 */
export interface CreditGaugeProps {
  readonly build: MeterReading
  readonly runtime: MeterReading
}

/** Written out per state so Tailwind can see the class names in the source. */
const FILL: Record<StatusState, string> = {
  agent: "bg-agent",
  waiting: "bg-waiting",
  live: "bg-live",
}

/**
 * The hold, drawn as hatching.
 *
 * A hold is money committed to a turn that has not settled yet (SPEC §16.2).
 * Texture is the right encoding for "provisional": a second solid colour would
 * read as a fourth state, and SPEC §18 allows three.
 *
 * `repeating-linear-gradient` rather than an SVG or an image, so the stripes
 * are painted out of the same custom properties as the bar they sit in. When
 * the approved mockup lands and `palette.css` changes, the hatching changes
 * with it and nothing here is touched.
 *
 * Static, with no animation and nothing that transitions its background: a
 * hold that crawls would pull the eye continuously while someone is trying to
 * work, and the spend it represents is not moving anyway — it is waiting.
 */
const HATCH: Record<StatusState, CSSProperties> = {
  agent: {
    backgroundImage:
      "repeating-linear-gradient(135deg, var(--color-agent) 0 2px, var(--color-agent-soft) 2px 5px)",
  },
  waiting: {
    backgroundImage:
      "repeating-linear-gradient(135deg, var(--color-waiting) 0 2px, var(--color-waiting-soft) 2px 5px)",
  },
  live: {
    backgroundImage:
      "repeating-linear-gradient(135deg, var(--color-live) 0 2px, var(--color-live-soft) 2px 5px)",
  },
}

/** Width is the only thing that animates, and only for users who want motion. */
const GROWS = "h-full motion-safe:transition-[width] motion-safe:duration-300 motion-safe:ease-out"

const TRACK = "h-2 w-36 rounded-sm"

function UnknownMeter({ name }: { readonly name: string }) {
  return (
    <>
      <span className="text-xs font-medium text-ink">{name}</span>
      {/*
        Not "0". Zero is a lie that reads as "you are out of credits", and this
        meter is not empty — it is unmeasured, because the credits API does not
        exist before phase 4. Not a spinner either: nothing is loading.
      */}
      <span className="text-xs text-ink-muted">Not measured yet</span>
      {/* Dashed and empty: a track with no reading, not a reading of nothing. */}
      <span aria-hidden="true" className={`${TRACK} border border-dashed border-border`} />
      <span />
    </>
  )
}

function Meter({
  name,
  reading,
  tone,
  low,
}: {
  readonly name: string
  readonly reading: MeterReading
  readonly tone: StatusState
  readonly low: boolean
}) {
  if (reading.kind === "unknown") return <UnknownMeter name={name} />

  const meter = meterDisplay(reading)
  const held = meter.hold > 0 ? `, ${formatCredits(meter.hold)} on hold` : ""
  const running = low ? ", running low" : ""

  return (
    <>
      <span className="text-xs font-medium text-ink">{name}</span>
      <span className="text-xs tabular-nums text-ink-muted">
        {formatCredits(meter.used)} of {formatCredits(meter.allowance)}
        {meter.hold > 0 ? ` · ${formatCredits(meter.hold)} on hold` : ""}
      </span>
      <span
        role="progressbar"
        /*
          The bars are visual, so the numbers have to be in the name. `valuenow`
          is the committed total — settled plus held — because that is where the
          fill reaches, and it is clamped into range: an overdraw should never
          be possible (SPEC §16.2 reserves before spending) and an out-of-range
          progressbar is invalid ARIA. The true figures stay in the label.
        */
        aria-label={`${name} credits: ${formatCredits(meter.used)} of ${formatCredits(meter.allowance)} used${held}${running}`}
        aria-valuemin={0}
        aria-valuemax={meter.allowance}
        aria-valuenow={Math.min(meter.committed, meter.allowance)}
        className={`${TRACK} flex overflow-hidden bg-surface-sunken`}
      >
        <span className={`${GROWS} ${FILL[tone]}`} style={{ width: `${meter.usedPercent}%` }} />
        <span
          data-hold="true"
          className={GROWS}
          style={{ ...HATCH[tone], width: `${meter.holdPercent}%` }}
        />
      </span>
      {/*
        Polite, and containing only the badge, so crossing SPEC §16.3's 80%
        rung is announced once. Announcing every credit as it burns would make
        the gauge unusable with a screen reader.
      */}
      <span aria-live="polite">{low ? <Status state="waiting">Running low</Status> : null}</span>
    </>
  )
}

export function CreditGauge({ build, runtime }: CreditGaugeProps) {
  const runtimeLow = isRuntimeLow(runtime)
  return (
    <div className="grid grid-cols-[auto_auto_auto_auto] items-center gap-x-2 gap-y-1">
      {/*
        Build never warns. SPEC §16.3: build exhaustion pauses the builder and
        is harmless, so there is nothing here for the user to fix and nothing
        amber to say about it.
      */}
      <Meter name="Build" reading={build} tone="agent" low={false} />
      <Meter
        name="Runtime"
        reading={runtime}
        tone={runtimeLow ? "waiting" : "live"}
        low={runtimeLow}
      />
    </div>
  )
}
