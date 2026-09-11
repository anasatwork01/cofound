import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Status, type StatusState } from "./status"

const STATES: readonly StatusState[] = ["agent", "waiting", "live"]

describe("Status", () => {
  it("renders each of the three states, and labels it in words", () => {
    for (const [state, word] of [
      ["agent", "Agent"],
      ["waiting", "Waiting on you"],
      ["live", "Live"],
    ] as const) {
      const { container, unmount } = render(<Status state={state} />)
      const badge = container.firstElementChild
      expect(badge?.getAttribute("data-state")).toBe(state)
      expect(badge?.textContent).toBe(word)
      unmount()
    }
  })

  it("puts the state into the accessible name even when the label does not", () => {
    // SPEC §18's three states are used identically everywhere, so the state has
    // to be announced whatever the situation happens to be called.
    const { container } = render(<Status state="waiting">Approve the ad budget</Status>)
    expect(container.firstElementChild?.textContent).toBe("Waiting on you: Approve the ad budget")
    // And the situation is still there for a sighted reader.
    expect(screen.getByText("Approve the ad budget")).toBeTruthy()
  })

  it("distinguishes the three states without using colour", () => {
    // What a colour-blind user, a greyscale print and a forced high-contrast
    // palette all still have: a different silhouette and a different word.
    const glyphs = new Set<string>()
    const words = new Set<string>()
    for (const state of STATES) {
      const { container, unmount } = render(<Status state={state}>Same words every time</Status>)
      glyphs.add(container.querySelector("svg")?.innerHTML ?? "")
      words.add(container.querySelector(".sr-only")?.textContent ?? "")
      unmount()
    }
    expect(glyphs.size).toBe(3)
    expect(words.size).toBe(3)
  })

  it("hides the glyph from assistive technology, because the words already say it", () => {
    const { container } = render(<Status state="live">Serving traffic</Status>)
    const glyph = container.querySelector("svg")
    expect(glyph?.getAttribute("aria-hidden")).toBe("true")
    expect(glyph?.getAttribute("focusable")).toBe("false")
  })

  it("is a label, not a live region: announcing a static badge would interrupt", () => {
    const { container } = render(<Status state="agent">Drafting a campaign</Status>)
    expect(container.querySelector("[aria-live]")).toBeNull()
    expect(container.querySelector("[role]")).toBeNull()
  })
})
