import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { CreditGauge } from "./credit-gauge"
import { RUNTIME_WARNING_PERCENT, UNKNOWN_CREDITS, type MeterReading } from "./credits"

/** One meter's worth of "there is no credits endpoint before phase 4". */
const UNKNOWN: MeterReading = { kind: "unknown" }

/**
 * SPEC §18 requires reduced motion to be respected. Tailwind's `motion-safe:`
 * variant is the mechanism, so the assertion is that nothing animates without
 * it — checked structurally rather than by reading a class name we happen to
 * remember writing.
 */
function motionClasses(root: HTMLElement): string[] {
  const animates = /(^|:)(transition|duration|ease|animate)(-|$)/
  return [...root.querySelectorAll<HTMLElement>("*")]
    .flatMap((el) => [...el.classList])
    .filter((token) => animates.test(token))
}

/**
 * The tracks, in document order: the instrument itself, measured or unmeasured.
 * Both states carry the marker, because several of the rules below are about
 * the two being THE SAME OBJECT in different conditions.
 */
function tracks(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>("[data-track]")]
}

/** The figures cell belonging to one meter, found from its bar. */
function readout(bar: HTMLElement): string {
  const name = bar.getAttribute("aria-label")?.split(" ")[0] ?? ""
  return bar.parentElement?.querySelector(`[data-readout="${name}"]`)?.textContent ?? ""
}

function classesOf(root: HTMLElement): string[] {
  return [root, ...root.querySelectorAll<HTMLElement>("*")].flatMap((el) => [...el.classList])
}

describe("CreditGauge", () => {
  it("shows build and runtime as two separate meters", () => {
    // SPEC §16.3 gives them different exhaustion behaviours; they are not one
    // meter split in two.
    render(
      <CreditGauge
        build={{ kind: "measured", used: 1200, allowance: 4000 }}
        runtime={{ kind: "measured", used: 100, allowance: 4000 }}
      />,
    )
    expect(screen.getByRole("progressbar", { name: /^Build credits:/ })).toBeTruthy()
    expect(screen.getByRole("progressbar", { name: /^Runtime credits:/ })).toBeTruthy()
  })

  it("carries the real numbers in the accessible name, since the bars are visual", () => {
    render(
      <CreditGauge
        build={{ kind: "measured", used: 1234, allowance: 4000, hold: 40 }}
        runtime={{ kind: "measured", used: 0, allowance: 4000 }}
      />,
    )
    const build = screen.getByRole("progressbar", { name: /^Build credits:/ })
    expect(build.getAttribute("aria-label")).toBe("Build credits: 1,234 of 4,000 used, 40 on hold")
    expect(build.getAttribute("aria-valuemin")).toBe("0")
    expect(build.getAttribute("aria-valuemax")).toBe("4000")
    expect(build.getAttribute("aria-valuenow")).toBe("1274")
    // And on screen, in words a user can read while the number moves.
    //
    // Read off the cell rather than through `getByText`: the figure spent is a
    // span of its own now, so that the amount carries the weight and what it is
    // spent against stays muted, and `getByText` only ever sees an element's
    // OWN text nodes. An equality on the cell's text is the stronger assertion
    // anyway — it fails on an extra word as well as on a missing one.
    expect(readout(screen.getByRole("progressbar", { name: /^Build credits:/ }))).toBe(
      "1,234 of 4,000 · 40 on hold",
    )
    expect(screen.getByText("Build")).toBeTruthy()
    expect(screen.getByText("Runtime")).toBeTruthy()
  })

  it("draws the active hold as static hatching over the filled part of the bar", () => {
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 2000, allowance: 4000, hold: 400 }}
        runtime={{ kind: "measured", used: 0, allowance: 4000 }}
      />,
    )
    const hold = container.querySelector<HTMLElement>('[data-hold="true"]')
    expect(hold).not.toBeNull()
    const image = hold?.style.backgroundImage ?? ""
    // A gradient, so it is painted from the tokens and swaps with them.
    expect(image).toContain("repeating-linear-gradient")
    expect(image).toContain("var(--color-agent)")
    expect(image).not.toContain("url(")
    // Sized from the reading, and adjacent to the settled segment.
    expect(hold?.style.width).toBe("10%")
    expect(hold?.previousElementSibling).not.toBeNull()
    // Static: a hold that crawls pulls the eye while someone is trying to work.
    expect(image).not.toContain("animation")
    expect(container.innerHTML).not.toContain("animate-")
  })

  it("leaves the hatching out when nothing is held", () => {
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 2000, allowance: 4000 }}
        runtime={{ kind: "measured", used: 0, allowance: 4000 }}
      />,
    )
    expect(container.querySelector<HTMLElement>('[data-hold="true"]')?.style.width).toBe("0%")
  })

  it("warns on runtime at exactly 80 percent and not below", () => {
    const low = render(
      <CreditGauge
        build={{ kind: "measured", used: 0, allowance: 4000 }}
        runtime={{ kind: "measured", used: 3200, allowance: 4000 }}
      />,
    )
    // SPEC §18: amber is "waiting on you", and topping up is the user's move.
    expect(low.getByText("Running low")).toBeTruthy()
    expect(low.container.querySelector('[data-state="waiting"]')).not.toBeNull()
    expect(
      low.getByRole("progressbar", { name: /^Runtime credits:/ }).getAttribute("aria-label"),
    ).toContain("running low")
    low.unmount()

    const fine = render(
      <CreditGauge
        build={{ kind: "measured", used: 0, allowance: 4000 }}
        runtime={{ kind: "measured", used: 3199, allowance: 4000 }}
      />,
    )
    expect(fine.queryByText("Running low")).toBeNull()
  })

  it("never warns on build, which runs out harmlessly", () => {
    // SPEC §16.3: build exhaustion pauses the builder. There is nothing to fix.
    const { queryByText, container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 4000, allowance: 4000 }}
        runtime={{ kind: "measured", used: 0, allowance: 4000 }}
      />,
    )
    expect(queryByText("Running low")).toBeNull()
    expect(container.querySelector('[data-state="waiting"]')).toBeNull()
  })

  it("announces the warning once, politely, rather than every credit as it burns", () => {
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 0, allowance: 4000 }}
        runtime={{ kind: "measured", used: 3200, allowance: 4000 }}
      />,
    )
    const live = container.querySelectorAll("[aria-live]")
    expect(live).toHaveLength(2)
    for (const region of live) {
      expect(region.getAttribute("aria-live")).toBe("polite")
      expect(region.querySelector("[role=progressbar]")).toBeNull()
    }
  })

  it("says it has no reading rather than drawing a zero", () => {
    // There is no credits endpoint before phase 4. Zero would read as "you are
    // out of credits", and a spinner would claim something is on its way.
    const { container } = render(<CreditGauge {...UNKNOWN_CREDITS} />)
    expect(screen.getAllByText("Not measured yet")).toHaveLength(2)
    expect(screen.queryAllByRole("progressbar")).toHaveLength(0)
    expect(container.textContent).toBe("BuildNot measured yetRuntimeNot measured yet")
    expect(container.textContent).not.toContain("0")
  })

  it("renders one unknown meter beside one measured meter", () => {
    render(
      <CreditGauge
        build={{ kind: "measured", used: 10, allowance: 4000 }}
        runtime={{ kind: "unknown" }}
      />,
    )
    expect(screen.getAllByText("Not measured yet")).toHaveLength(1)
    expect(screen.getAllByRole("progressbar")).toHaveLength(1)
  })

  it("never lets a fraction reach the DOM", () => {
    // SPEC §16.6. Text, ARIA values and bar widths all go through the same
    // rounding, so this scans the whole rendered output rather than one label.
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 33.333, allowance: 100.5, hold: 0.4 }}
        runtime={{ kind: "measured", used: 0.0031, allowance: 4000.77 }}
      />,
    )
    expect(container.innerHTML).not.toMatch(/\d\.\d/)
    expect(readout(screen.getByRole("progressbar", { name: /^Build credits:/ }))).toBe(
      // The 0.4 hold rounds away entirely, so the clause goes with it: the bar
      // and the sentence are derived from the same rounded figures.
      "33 of 101",
    )
  })

  it("animates only for users who have not asked it to stop", () => {
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 1200, allowance: 4000, hold: 100 }}
        runtime={{ kind: "measured", used: 3600, allowance: 4000 }}
      />,
    )
    const motion = motionClasses(container)
    // There is something to guard, and all of it is guarded.
    expect(motion.length).toBeGreaterThan(0)
    for (const token of motion) {
      expect(token.startsWith("motion-safe:")).toBe(true)
    }
  })

  it("draws the track with square ends, in every state", () => {
    // SPEC §3.1's mockup: "square ends, deliberately: a rounded cap makes a
    // nearly-empty bar unreadable at exactly the moment the reading matters",
    // and the radius scale's own rule — zero radius means an instrument you
    // read. So this is not an assertion about a spelling: NOTHING inside a
    // track may carry a radius at all.
    //
    // It has already gone wrong once, silently. The track said `rounded-sm`
    // while that step was 3px; the 2026-09-12 warming moved it to 8px and an
    // 8px-tall track became a capsule, with no component change to review.
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 40, allowance: 4000, hold: 80 }}
        runtime={{ kind: "unknown" }}
      />,
    )
    expect(tracks(container)).toHaveLength(2)
    const rounded = tracks(container)
      .flatMap(classesOf)
      .filter((token) => /(^|:)rounded(-|$)/.test(token))
    expect(rounded, "a radius inside the gauge: the instrument reads square").toEqual([])
  })

  it("draws the same instrument whether or not it has a reading", () => {
    // The unknown meter used to be a dashed OUTLINE the width of a track; it is
    // now a real track with a dashed centreline. That matters beyond taste: the
    // gauge sits in persistent chrome, and an instrument that changes shape
    // when the first reading arrives moves the top bar under the user.
    const { container } = render(
      <CreditGauge build={{ kind: "measured", used: 40, allowance: 4000 }} runtime={UNKNOWN} />,
    )
    const geometry = (track: HTMLElement) =>
      [...track.classList].filter((token) => /^[hw]-/.test(token)).sort()
    const [measured, unmeasured] = tracks(container)
    expect(geometry(measured!)).not.toEqual([])
    expect(geometry(unmeasured!)).toEqual(geometry(measured!))
  })

  it("graduates runtime and not build, because only runtime has a threshold", () => {
    // SPEC §16.3, and the mockup's own argument: build exhaustion pauses the
    // builder while you are at the keyboard; runtime exhaustion reaches your
    // customers. Graduating both would say they are the same instrument.
    render(
      <CreditGauge
        build={{ kind: "measured", used: 1200, allowance: 4000 }}
        runtime={{ kind: "measured", used: 1200, allowance: 4000 }}
      />,
    )
    const build = screen.getByRole("progressbar", { name: /^Build credits:/ })
    const runtime = screen.getByRole("progressbar", { name: /^Runtime credits:/ })
    expect(build.querySelectorAll("[data-grad], [data-tick]")).toHaveLength(0)
    expect(
      [...runtime.querySelectorAll<HTMLElement>("[data-grad]")].map((mark) => mark.style.left),
    ).toEqual(["25%", "50%", "75%"])
  })

  it("puts the tick where the warning actually starts", () => {
    // Not "at 80%". The mark is drawn from RUNTIME_WARNING_PERCENT and the
    // warning is decided from the same constant, so this renders the reading
    // that constant describes and asserts both ends of it at once: a tick
    // at the same position, and the badge that position promises.
    const allowance = 4000
    render(
      <CreditGauge
        build={UNKNOWN}
        runtime={{
          kind: "measured",
          used: (allowance * RUNTIME_WARNING_PERCENT) / 100,
          allowance,
        }}
      />,
    )
    const runtime = screen.getByRole("progressbar", { name: /^Runtime credits:/ })
    expect(runtime.querySelector<HTMLElement>("[data-tick]")?.style.left).toBe(
      `${RUNTIME_WARNING_PERCENT}%`,
    )
    expect(screen.getByText("Running low")).toBeTruthy()
  })

  it("draws a hold as one claim, in one colour, in both bars", () => {
    // SPEC §16.2's hold is the agent's claim on money not yet spent, so it
    // wears the agent's colour wherever it appears — SPEC §18's "one learned
    // pattern, not three" applied to the one mark that can show up in either
    // meter. Tinting it per meter made "hold" two patterns sharing a texture.
    const { container } = render(
      <CreditGauge
        build={{ kind: "measured", used: 1000, allowance: 4000, hold: 400 }}
        runtime={{ kind: "measured", used: 1000, allowance: 4000, hold: 400 }}
      />,
    )
    const [build, runtime] = [...container.querySelectorAll<HTMLElement>('[data-hold="true"]')]
    expect(build?.style.backgroundImage).toContain("var(--color-agent)")
    expect(runtime?.style.backgroundImage).toBe(build?.style.backgroundImage)
    // And drawn on unspent track: the gaps let the track through rather than
    // painting a second colour, so the pair that renders is violet on the
    // sunken track and never violet on teal.
    expect(build?.style.backgroundImage).toContain("transparent")
  })

  it("reads the figures in the voice reserved for measurement", () => {
    // The token layer's rule, verbatim: "MONO IS THE VOICE OF MEASUREMENT AND
    // NEVER OF LABELS: credits, costs, durations, versions, positions, SHAs —
    // anything compared down a column. Not captions, not eyebrows, not prose."
    // Tabular figures come with it, which is what stops the digits beside a
    // changing number from shuffling.
    const { container } = render(
      <CreditGauge build={{ kind: "measured", used: 1200, allowance: 4000 }} runtime={UNKNOWN} />,
    )
    const figures = container.querySelector<HTMLElement>('[data-readout="Build"]')
    expect([...(figures?.classList ?? [])]).toEqual(
      expect.arrayContaining(["font-mono", "tabular-nums"]),
    )
    // The name beside it is a label, so it is not in that voice, and neither is
    // the unmeasured meter's sentence.
    for (const prose of [screen.getByText("Build"), screen.getByText("Not measured yet")]) {
      expect([...prose.classList]).not.toContain("font-mono")
    }
  })
})
