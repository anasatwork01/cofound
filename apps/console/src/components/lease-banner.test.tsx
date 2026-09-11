import { act, fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { LeaseBanner } from "@/components/lease-banner"
import { selectEditorMode, useBuilderStore, type Lease } from "@/store/builder-store"

function applyLease(lease: Lease): void {
  act(() => {
    useBuilderStore.getState().applyLease(lease)
  })
}

beforeEach(() => {
  act(() => {
    useBuilderStore.getState().reset()
  })
})

describe("LeaseBanner", () => {
  it("says nothing while the editor is yours, but keeps the region to say it in", () => {
    render(<LeaseBanner />)
    const region = screen.getByRole("status")
    expect(region).toHaveAttribute("aria-live", "polite")
    expect(region).toBeEmptyDOMElement()
  })

  it("announces the change into the region that was already there", () => {
    // A polite live region only announces content added to a region assistive
    // technology was already observing. So the test has to watch the same DOM
    // node across the change, not just find a region with the sentence in it:
    // a banner that mounts complete with its text passes the second check and
    // announces nothing.
    render(<LeaseBanner />)
    const region = screen.getByRole("status")
    expect(region).toBeEmptyDOMElement()

    applyLease({ holder: "agent" })

    expect(screen.getByRole("status")).toBe(region)
    expect(region).toHaveTextContent(/read-only/)
    expect(selectEditorMode(useBuilderStore.getState())).toBe("read-only")
  })

  it("offers a way out rather than silently rejecting edits", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    render(<LeaseBanner />)

    fireEvent.click(screen.getByRole("button", { name: "Take over" }))

    // The action keeps its name through the flow: Take over -> Taking over.
    expect(screen.getByRole("button", { name: "Taking over" })).toBeDisabled()
    expect(screen.getByRole("status")).toHaveTextContent(/comes back to you/)
    expect(useBuilderStore.getState().lease.pending_holder).toBe("human")
  })

  it("gets out of the way once the lease is handed over, region included", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    render(<LeaseBanner />)
    const region = screen.getByRole("status")
    expect(region).toHaveTextContent(/read-only/)

    applyLease({ holder: "human" })

    // The sentence goes; the region stays, ready for the next change of hands.
    expect(screen.getByRole("status")).toBe(region)
    expect(region).toBeEmptyDOMElement()
    expect(screen.queryByRole("button", { name: "Take over" })).not.toBeInTheDocument()
  })
})
