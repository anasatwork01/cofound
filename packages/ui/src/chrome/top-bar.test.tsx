import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { TopBar, type TopBarProps } from "./top-bar"

describe("TopBar", () => {
  it("is a banner landmark carrying the mark and the gauge", () => {
    // SPEC §18: the credit gauge is in the top bar on every screen.
    render(<TopBar />)
    const banner = screen.getByRole("banner")
    expect(banner.contains(screen.getByRole("link", { name: "Halyard" }))).toBe(true)
    expect(screen.getAllByText("Not measured yet")).toHaveLength(2)
  })

  it("defaults both meters to unknown, which is all phase 0 can honestly say", () => {
    const { container } = render(<TopBar />)
    expect(container.querySelectorAll("[role=progressbar]")).toHaveLength(0)
  })

  it("shows real readings when it is given them", () => {
    render(
      <TopBar
        credits={{
          build: { kind: "measured", used: 1200, allowance: 4000, hold: 40 },
          runtime: { kind: "measured", used: 100, allowance: 4000 },
        }}
      />,
    )
    expect(screen.getByRole("progressbar", { name: /^Build credits:/ })).toBeTruthy()
    expect(screen.getByRole("progressbar", { name: /^Runtime credits:/ })).toBeTruthy()
  })

  it("points the mark at the route that decides where you belong", () => {
    // SPEC §18: `/` redirects to the last project, or to `/new`.
    render(<TopBar />)
    expect(screen.getByRole("link", { name: "Halyard" }).getAttribute("href")).toBe("/")
  })

  it("takes focus from the keyboard and shows a ring when it has it", () => {
    render(<TopBar />)
    const mark = screen.getByRole("link", { name: "Halyard" })
    mark.focus()
    expect(document.activeElement).toBe(mark)
    expect(mark.getAttribute("tabindex")).toBeNull()
    // SPEC §18 requires a visible focus ring, drawn from the focus token.
    expect(mark.className).toContain("focus-visible:outline-2")
    expect(mark.className).toContain("outline-focus")
  })

  it("reads the org and project as text, since there is no switcher to open yet", () => {
    render(<TopBar org="Northwind" project="storefront" />)
    expect(screen.getByText("Northwind")).toBeTruthy()
    expect(screen.getByText("storefront")).toBeTruthy()
    // No button, no menu: there is no API behind either of them.
    expect(screen.queryAllByRole("button")).toHaveLength(0)
  })

  it("says nothing about context it has not been given", () => {
    const { container } = render(<TopBar />)
    expect(container.querySelector("p")).toBeNull()
  })

  it("hands the context slot over whole, for the switcher to land in", () => {
    render(
      <TopBar
        org="Northwind"
        project="storefront"
        contextSlot={<button>Northwind / storefront</button>}
      />,
    )
    expect(screen.getByRole("button", { name: "Northwind / storefront" })).toBeTruthy()
    // The slot replaces the text; the bar does not render both.
    expect(screen.queryByText("Northwind", { selector: "span" })).toBeNull()
  })

  it("is one fixed height, whatever the gauge is currently saying", () => {
    // SPEC §18's persistent chrome. The credit gauge's tallest state adds a
    // "Running low" chip to its second row, and a bar sized by its content
    // would grow by that chip's height — shoving every screen down at the
    // moment the user is reading the thing that caused it.
    //
    // So the height is declared, not derived: one `h-*`, no `min-h-*` and no
    // vertical padding for the content to push against.
    const barFor = (credits?: TopBarProps["credits"]) => {
      const { container, unmount } = render(<TopBar {...(credits ? { credits } : {})} />)
      const className = container.querySelector("header")?.className ?? ""
      unmount()
      return className.split(/\s+/)
    }
    const quiet = barFor()
    const low = barFor({
      build: { kind: "measured", used: 1200, allowance: 4000, hold: 40 },
      runtime: { kind: "measured", used: 3900, allowance: 4000 },
    })
    expect(low).toEqual(quiet)
    expect(quiet.filter((token) => /^h-/.test(token))).toHaveLength(1)
    expect(quiet.filter((token) => /^(min-h-|max-h-|py-|p-)/.test(token))).toEqual([])
  })
})
