import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { CreditGauge } from "./credit-gauge"
import { UNKNOWN_CREDITS } from "./credits"

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
    expect(screen.getByText("1,234 of 4,000 · 40 on hold")).toBeTruthy()
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
    expect(screen.getByText(/^33 of 101/)).toBeTruthy()
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
})
