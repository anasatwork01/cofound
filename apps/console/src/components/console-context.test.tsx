import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const router = vi.hoisted(() => ({ replace: vi.fn(), push: vi.fn() }))
vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => "/new",
  useSearchParams: () => new URLSearchParams(),
}))

import { ConsoleContext } from "@/components/console-context"
import { CURRENT_ORG_KEY } from "@/lib/current-org"
import { envelope, renderWithQuery, sessionBody, stubApi } from "@/test-utils"

const TWO_ORGS = sessionBody([
  { slug: "acme", name: "Acme" },
  { slug: "beta-works", name: "Beta Works" },
])

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  router.replace.mockClear()
  window.localStorage.clear()
})

describe("ConsoleContext", () => {
  it("says nothing at all until the session has answered", () => {
    stubApi({ "/auth/session": { body: TWO_ORGS } })
    const { container } = renderWithQuery(<ConsoleContext />)
    // It is in the top bar, on every screen. A control that appeared, said
    // "signed out", and then became an org name would flash the wrong answer on
    // every page load in the product.
    expect(container.textContent).toBe("")
  })

  it("says nothing to a signed-out reader either", async () => {
    stubApi({
      "/auth/session": {
        status: 401,
        body: envelope({ code: "unauthenticated", message: "You are not signed in." }),
      },
    })
    const { container } = renderWithQuery(<ConsoleContext />)
    await waitFor(() => {
      expect(container.querySelector("button")).toBeNull()
    })
    expect(container.textContent).toBe("")
  })

  it("names the org as text when there is only one to be in", async () => {
    stubApi({ "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) } })
    renderWithQuery(<ConsoleContext />)
    expect(await screen.findByText("Acme")).toBeVisible()
    // No control, because there is no choice to make.
    expect(screen.queryByRole("combobox")).toBeNull()
  })

  it("offers SPEC §8's org switch when there is more than one", async () => {
    stubApi({ "/auth/session": { body: TWO_ORGS } })
    renderWithQuery(<ConsoleContext />)
    const picker = await screen.findByRole("combobox", { name: "Organisation" })
    expect(picker).toHaveValue("acme")
    expect([...picker.querySelectorAll("option")].map((o) => o.textContent)).toEqual([
      "Acme",
      "Beta Works",
    ])
  })

  it("has a real label, not a placeholder option", async () => {
    // axe's `label` rule accepts a non-empty placeholder, so this is asserted
    // rather than left to the auditor.
    stubApi({ "/auth/session": { body: TWO_ORGS } })
    const { container } = renderWithQuery(<ConsoleContext />)
    const picker = await screen.findByRole("combobox", { name: "Organisation" })
    expect(picker.id).not.toBe("")
    expect(container.querySelector(`label[for="${picker.id}"]`)).not.toBeNull()
  })

  it("remembers the switch, so a reload lands in the same org", async () => {
    stubApi({ "/auth/session": { body: TWO_ORGS } })
    renderWithQuery(<ConsoleContext />)
    const picker = await screen.findByRole("combobox", { name: "Organisation" })

    fireEvent.change(picker, { target: { value: "beta-works" } })

    await waitFor(() => {
      expect(picker).toHaveValue("beta-works")
    })
    expect(window.localStorage.getItem(CURRENT_ORG_KEY)).toBe("beta-works")
  })

  it("signs out through the API and clears what the last user left behind", async () => {
    const { calls } = stubApi({
      "/auth/session": { body: TWO_ORGS },
    })
    const { queryClient } = renderWithQuery(<ConsoleContext />)
    const clear = vi.spyOn(queryClient, "clear")

    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }))

    await waitFor(() => {
      expect(calls.some((call) => call.method === "DELETE")).toBe(true)
    })
    expect(calls.find((call) => call.method === "DELETE")?.url).toContain("/auth/session")
    await waitFor(() => {
      // Both halves matter: a cache that survived would render the last user's
      // orgs on the next screen, and `/` would read a stale session and send
      // them straight back in.
      expect(clear).toHaveBeenCalled()
      expect(router.replace).toHaveBeenCalledWith("/signin")
    })
  })

  it("still gets the reader out when the sign-out request fails", async () => {
    stubApi({
      "/auth/session": { body: TWO_ORGS },
    })
    const { queryClient } = renderWithQuery(<ConsoleContext />)
    await screen.findByRole("button", { name: "Sign out" })

    // The DELETE fails after the session GET has already answered.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("Failed to fetch")
      }),
    )
    const clear = vi.spyOn(queryClient, "clear")
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }))

    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/signin")
    })
    expect(clear).toHaveBeenCalled()
  })

  it("keeps the action's name while it is happening", async () => {
    stubApi({ "/auth/session": { body: TWO_ORGS } })
    renderWithQuery(<ConsoleContext />)
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }))
    await waitFor(() => {
      expect(document.body.textContent ?? "").toMatch(/Signing out|Sign out/)
    })
  })
})
