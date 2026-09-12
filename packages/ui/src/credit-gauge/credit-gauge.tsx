import type { CSSProperties } from "react"
import { Status, type StatusState } from "../status/status"
import {
  RUNTIME_WARNING_PERCENT,
  formatCredits,
  isRuntimeLow,
  meterDisplay,
  type MeterReading,
} from "./credits"

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
 * THE GAUGE IS AN INSTRUMENT, AND IT IS THE ONE THING THE WARMER REGISTER DOES
 * NOT SOFTEN. The 2026-09-12 direction change (docs/mockup.html, "Why the
 * direction changed") stepped radius, padding and weight up across the console;
 * here it bought three things and nothing else — the mockup's own column rhythm
 * (space-3 across, space-2 down), a wider track, and the readout moved into the
 * measuring voice. Density stayed tight and the four rules below did not move.
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
 * RULE 1 — THE TRACK HAS SQUARE ENDS, AND THE ABSENCE OF A RADIUS IS THE POINT.
 *
 * `docs/mockup.html`: "square ends, deliberately: a rounded cap makes a
 * nearly-empty bar unreadable at exactly the moment the reading matters", and
 * the radius scale's own rule — zero for instruments and data surfaces, the
 * things you read. This class list therefore names no radius at all, and
 * `credit-gauge.test.tsx` fails if anything inside a track acquires one.
 *
 * It is not a hypothetical. This said `rounded-sm` until the warming moved that
 * step from 3px to 8px, at which point an 8px-tall track became a capsule and
 * the first 16px of every reading turned into a lozenge — a token edit, no
 * component change, nothing red.
 *
 * The width is the mockup's: 10.5rem of track inside its 21rem top-bar gauge.
 */
const TRACK = "h-2 w-42 bg-surface-sunken"

/**
 * RULE 2 — A HOLD IS VIOLET IN BOTH BARS, AND IT IS THE SAME PATTERN IN BOTH.
 *
 * A hold is money committed to a turn that has not settled yet (SPEC §16.2) —
 * the agent's claim on money not yet spent. So it wears the agent's colour
 * wherever it appears, exactly as SPEC §18 asks of every other state mark:
 * "one learned pattern, not three". Tinting it per meter, which this used to
 * do, made "hold" two patterns that happened to share a texture.
 *
 * RULE 3 — NOTHING COLOURED IS DRAWN ON SPENT FILL. The hold sits on unspent
 * track only, so the pair that actually renders is violet on the sunken track
 * (4.58:1) rather than violet on teal (1.01:1). The gaps are `transparent`
 * rather than a tint for the same reason: whatever the hold is drawn over shows
 * through it, and the texture stays legible without a second opinion about the
 * background.
 *
 * RULE 4 — IT DOES NOT MOVE. Static gradient, no animation, nothing
 * transitioning its background: a hold that crawled would pull the eye
 * continuously while someone is trying to work, and the spend it represents is
 * not moving anyway — it is waiting.
 *
 * `repeating-linear-gradient` rather than an SVG or an image, so the stripes are
 * painted out of the same custom property as everything else: when the mockup
 * remaps `--color-agent`, the hatching follows and nothing here is touched.
 */
const HOLD: CSSProperties = {
  backgroundImage:
    "repeating-linear-gradient(135deg, var(--color-agent) 0 2px, transparent 2px 5px)",
}

/**
 * The unknown reading: the instrument is drawn, the reading is absent.
 *
 * A dashed centreline down a real track, not a dashed outline standing in for
 * one — the track is the same object in every state, so the top bar does not
 * change shape when credits arrive. Same reasoning as the em dash in the
 * mockup's unknown gauge, and the same reason "Not measured yet" is not a zero.
 */
const UNMEASURED: CSSProperties = {
  backgroundImage:
    "repeating-linear-gradient(to right, var(--color-border-strong) 0 3px, transparent 3px 6px)",
}

/** Width is the only thing that animates, and only for users who want motion. */
const GROWS = "h-full motion-safe:transition-[width] motion-safe:duration-300 motion-safe:ease-out"

/**
 * RULE 5 — ONLY RUNTIME IS GRADUATED, AND THE ASYMMETRY IS THE ARGUMENT.
 *
 * SPEC §16.3 gives the two meters different exhaustion behaviours. Running out
 * of build credit pauses the builder, which is harmless because you are at the
 * keyboard; running out of runtime credit reaches your app's own customers, so
 * it follows a policy ladder — and only it has a threshold worth watching
 * approach. Graduating both would say they are the same instrument.
 *
 * The quarter marks are for reading a proportion at a glance. The tick is
 * §16.3's first rung, drawn from `RUNTIME_WARNING_PERCENT` rather than from a
 * literal, so the mark and the warning can never end up in different places.
 *
 * Both are neutral, and the tick stays neutral after it is crossed. The mockup
 * turns it amber at that point; here the whole fill has already gone amber and
 * the badge has already appeared, so an amber tick would be amber on amber —
 * invisible at exactly the moment it is supposed to be read. Its job is to be
 * seen BEFORE you arrive at it, which neutral does.
 */
const QUARTERS = [25, 50, 75] as const

function Graduations() {
  return (
    <>
      {QUARTERS.map((percent) => (
        <span
          key={percent}
          data-grad={percent}
          className="absolute top-1/2 h-1 w-px -translate-y-1/2 bg-border-strong"
          style={{ left: `${percent}%` }}
        />
      ))}
      <span
        data-tick={RUNTIME_WARNING_PERCENT}
        className="absolute inset-y-0 w-[2px] bg-border-strong"
        style={{ left: `${RUNTIME_WARNING_PERCENT}%` }}
      />
    </>
  )
}

/**
 * The meter's name. Both states emit the same four cells — name, reading,
 * track, warning — so the gauge is one shape whatever it currently knows, and
 * the top bar does not reflow when the first real reading arrives.
 *
 * The name is the quiet half and the figures are the loud one: muted 500 here
 * against ink 600 there, which is the mockup's gauge label against its readout.
 * Weight and colour carry that hierarchy, never size — everything in this
 * instrument is `text-xs`, because an instrument is read as one object.
 */
function Name({ children }: { readonly children: string }) {
  return <span className="text-xs font-medium text-ink-muted">{children}</span>
}

function UnknownMeter({ name }: { readonly name: string }) {
  return (
    <>
      <Name>{name}</Name>
      {/*
        Not "0". Zero is a lie that reads as "you are out of credits", and this
        meter is not empty — it is unmeasured, because the credits API does not
        exist before phase 4. Not a spinner either: nothing is loading.
      */}
      <span data-readout={name} className="text-right text-xs text-ink-muted">
        Not measured yet
      </span>
      <span aria-hidden="true" data-track="unknown" className={`${TRACK} flex items-center`}>
        <span className="h-px w-full" style={UNMEASURED} />
      </span>
      <span />
    </>
  )
}

function Meter({
  name,
  reading,
  tone,
  low,
  graduated,
}: {
  readonly name: string
  readonly reading: MeterReading
  readonly tone: StatusState
  readonly low: boolean
  /** SPEC §16.3's threshold is runtime's alone. See `Graduations`. */
  readonly graduated: boolean
}) {
  if (reading.kind === "unknown") return <UnknownMeter name={name} />

  const meter = meterDisplay(reading)
  const held = meter.hold > 0 ? `, ${formatCredits(meter.hold)} on hold` : ""
  const running = low ? ", running low" : ""

  return (
    <>
      <Name>{name}</Name>
      {/*
        Mono, because the token layer reserves that voice for measurement —
        "credits, costs, durations, versions, positions, SHAs: anything compared
        down a column" — and tabular figures come with it, so a number that
        changes does not shuffle the digits beside it. The amount spent carries
        the weight; what it is spent against stays muted, which is the mockup's
        readout and its `small`.
      */}
      <span
        data-readout={name}
        className="text-right font-mono text-xs tabular-nums text-ink-muted"
      >
        <span className="font-semibold text-ink">{formatCredits(meter.used)}</span> of{" "}
        {formatCredits(meter.allowance)}
        {meter.hold > 0 ? ` · ${formatCredits(meter.hold)} on hold` : ""}
      </span>
      <span
        role="progressbar"
        data-track="measured"
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
        className={`${TRACK} relative flex overflow-hidden`}
      >
        <span className={`${GROWS} ${FILL[tone]}`} style={{ width: `${meter.usedPercent}%` }} />
        <span
          data-hold="true"
          className={GROWS}
          style={{ ...HOLD, width: `${meter.holdPercent}%` }}
        />
        {graduated ? <Graduations /> : null}
      </span>
      {/*
        Polite, and containing only the badge, so crossing SPEC §16.3's 80%
        rung is announced once. Announcing every credit as it burns would make
        the gauge unusable with a screen reader.

        `flex` is load-bearing geometry, not layout taste. As a block box this
        cell inherits the page's 15px/1.55 strut, so the row it sits in was 23px
        tall — taller than the 20px chip inside it — and the gauge measured 48px
        against a 52px bar. A flex container has no strut: the row is the chip.
        Measured in Chromium at the metric-matched fallback, which is the worst
        case: 42px unknown, 45px with the chip, inside 52px.
      */}
      <span aria-live="polite" className="flex">
        {low ? <Status state="waiting">Running low</Status> : null}
      </span>
    </>
  )
}

export function CreditGauge({ build, runtime }: CreditGaugeProps) {
  const runtimeLow = isRuntimeLow(runtime)
  return (
    /*
      Four columns: name, reading, track, warning. The gaps are the mockup's own
      — space-3 across, space-2 down — which is as far as the roomier register
      reaches into an instrument. Anything more and the two meters stop reading
      as one gauge, and the pair no longer fits the 52px top bar.
    */
    <div className="grid grid-cols-[auto_auto_auto_auto] items-center gap-x-3 gap-y-2">
      {/*
        Build never warns. SPEC §16.3: build exhaustion pauses the builder and
        is harmless, so there is nothing here for the user to fix and nothing
        amber to say about it.
      */}
      <Meter name="Build" reading={build} tone="agent" low={false} graduated={false} />
      <Meter
        name="Runtime"
        reading={runtime}
        tone={runtimeLow ? "waiting" : "live"}
        low={runtimeLow}
        graduated
      />
    </div>
  )
}
