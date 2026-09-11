import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { TabNav } from "@/components/tab-nav"
import { projectNav, settingsNav } from "@/lib/routes"

const pathname = vi.hoisted(() => ({ current: "/" }))
vi.mock("next/navigation", () => ({ usePathname: () => pathname.current }))

beforeEach(() => {
  pathname.current = "/"
})

describe("TabNav", () => {
  it("renders every project route SPEC §18 lists", () => {
    pathname.current = "/p/acme"
    render(<TabNav label="Project" items={projectNav("acme")} />)
    const links = screen.getAllByRole("link")
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/p/acme",
      "/p/acme/files",
      "/p/acme/history",
      "/p/acme/features",
      "/p/acme/ship",
      "/p/acme/ads",
      "/p/acme/search",
    ])
    expect(links.map((link) => link.textContent)).toEqual([
      "Builder",
      "Files",
      "History",
      "Features",
      "Ship",
      "Ads",
      "Search",
    ])
  })

  it("marks exactly one tab current, for a screen reader and not only in colour", () => {
    pathname.current = "/p/acme/ship"
    render(<TabNav label="Project" items={projectNav("acme")} />)
    const current = screen.getAllByRole("link").filter((link) => link.getAttribute("aria-current"))
    expect(current).toHaveLength(1)
    expect(current[0]).toHaveTextContent("Ship")
    expect(current[0]).toHaveAttribute("aria-current", "page")
  })

  it("does not leave Builder current on a sibling screen", () => {
    pathname.current = "/p/acme/files"
    render(<TabNav label="Project" items={projectNav("acme")} />)
    expect(screen.getByRole("link", { name: "Builder" })).not.toHaveAttribute("aria-current")
  })

  it("names the navigation, so there are not two unlabelled navs on a screen", () => {
    pathname.current = "/settings/team"
    render(<TabNav label="Settings" items={settingsNav} />)
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeInTheDocument()
  })
})
