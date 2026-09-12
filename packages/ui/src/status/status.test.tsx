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

  it("carries the three marks the approved mockup draws, on the right states", () => {
    // SPEC §3.1 makes `docs/mockup.html` the design system, and its `.pip`
    // block fixes the alphabet: violet circle, amber triangle, teal square.
    // The same three marks appear in the project rail, where they are learned
    // before they are needed — so a badge drawing a different silhouette for
    // the same state teaches two alphabets for SPEC §18's one pattern.
    //
    // This asserted only that the three differ from each other, which stayed
    // green through a mapping that had `live` drawing the circle the mockup
    // gives to `agent`, and `agent` drawing a diamond the mockup never draws.
    const mark = (state: StatusState) => {
      const { container, unmount } = render(<Status state={state} />)
      const glyph = container.querySelector("svg")?.firstElementChild
      const shape = glyph?.tagName.toLowerCase() ?? ""
      // A `path` is only a triangle if it has three vertices and closes: the
      // diamond this used to draw is the same tag with four. `H` counts — the
      // triangle's base is a horizontal lineto.
      const vertices = (glyph?.getAttribute("d") ?? "").match(/[MLHV]/g)?.length ?? 0
      const closed = /Z$/.test(glyph?.getAttribute("d") ?? "")
      unmount()
      return shape === "path" ? `${shape}:${vertices}${closed ? ":closed" : ""}` : shape
    }
    expect(mark("agent")).toBe("circle")
    expect(mark("waiting")).toBe("path:3:closed")
    expect(mark("live")).toBe("rect")
  })
})
