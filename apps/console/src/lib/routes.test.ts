import { describe, expect, it } from "vitest"
import { isCurrent, projectNav, routes, settingsNav } from "@/lib/routes"

describe("routes", () => {
  it("builds every SPEC §18 project route from a slug", () => {
    expect(projectNav("acme-shop").map((item) => item.href)).toEqual([
      "/p/acme-shop",
      "/p/acme-shop/files",
      "/p/acme-shop/history",
      "/p/acme-shop/features",
      "/p/acme-shop/ship",
      "/p/acme-shop/ads",
      "/p/acme-shop/search",
    ])
  })

  it("escapes a slug rather than interpolating it raw", () => {
    expect(routes.builder("a/b?c")).toBe("/p/a%2Fb%3Fc")
  })

  it("lists the three settings screens", () => {
    expect(settingsNav.map((item) => item.label)).toEqual(["Credits", "Team", "Connections"])
  })
})

describe("isCurrent", () => {
  const items = projectNav("p")

  it("does not light up Builder on a sibling screen", () => {
    expect(isCurrent("/p/p/files", "/p/p", items)).toBe(false)
    expect(isCurrent("/p/p/files", "/p/p/files", items)).toBe(true)
  })

  it("marks Builder current on the builder itself", () => {
    expect(isCurrent("/p/p", "/p/p", items)).toBe(true)
  })

  it("keeps the nearest ancestor current on a deeper path", () => {
    expect(isCurrent("/p/p/files/src/app.tsx", "/p/p/files", items)).toBe(true)
    expect(isCurrent("/p/p/files/src/app.tsx", "/p/p", items)).toBe(false)
  })

  it("marks nothing current on an unrelated path", () => {
    expect(items.every((item) => !isCurrent("/settings/team", item.href, items))).toBe(true)
  })
})
