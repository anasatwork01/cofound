import { cleanup, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const router = vi.hoisted(() => ({ replace: vi.fn(), push: vi.fn() }))
vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => "/",
  useSearchParams: () => new URLSearchParams(),
}))

import { HomeRouter, homeDestination } from "@/app/home-router"
import { envelope, renderWithQuery, sessionBody, stubApi } from "@/test-utils"

const SESSION = { user: { id: "u1", email: "a@b.c", name: null }, orgs: [] }

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  router.replace.mockClear()
  window.localStorage.clear()
})

describe("homeDestination", () => {
  it("sends a signed-out visitor to sign in", () => {
    expect(homeDestination({ status: "signed-out" })).toBe("/signin")
  })

  it("sends a signed-in user with no organisation to create one", () => {
    // Every screen SPEC §18 lists is scoped to an org. With none, /new cannot
    // list projects, cannot create one, and has nothing to show.
    expect(homeDestination({ status: "signed-in", session: SESSION })).toBe("/orgs/new")
  })

  it("sends a signed-in user with an organisation onward", () => {
    const session = {
      ...SESSION,
      orgs: [{ id: "o1", slug: "acme", name: "Acme", role: "owner" as const }],
    }
    expect(homeDestination({ status: "signed-in", session })).toBe("/new")
  })

  it("goes NOWHERE while the answer is still coming", () => {
    // The bug this replaced: `/` redirected to /new unconditionally, which is
    // right for one of the three people who can arrive and wrong for the other
    // two. Moving before the session answers is how a signed-in user lands on
    // the sign-in screen.
    expect(homeDestination({ status: "loading" })).toBeNull()
  })

  it("goes nowhere when the session could not be reached", () => {
    // Redirecting to sign-in here would be a guess dressed as a fact: the API
    // being unreachable is not evidence that the reader is signed out.
    expect(homeDestination({ status: "unreachable", error: new TypeError("offline") })).toBeNull()
  })
})

describe("HomeRouter", () => {
  it("replaces rather than pushes, so Back does not bounce", async () => {
    stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
    })
    renderWithQuery(<HomeRouter />)
    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/new")
    })
    expect(router.push).not.toHaveBeenCalled()
  })

  it("sends a signed-out visitor to sign in", async () => {
    stubApi({
      "/auth/session": {
        status: 401,
        body: envelope({ code: "unauthenticated", message: "You are not signed in." }),
      },
    })
    renderWithQuery(<HomeRouter />)
    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/signin")
    })
  })

  it("sends a signed-in visitor with no org to create one", async () => {
    stubApi({ "/auth/session": { body: sessionBody([]) } })
    renderWithQuery(<HomeRouter />)
    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/orgs/new")
    })
  })

  it("shows no destination's screen while it is deciding", () => {
    stubApi({ "/auth/session": { body: sessionBody([]) } })
    renderWithQuery(<HomeRouter />)
    // One heading, the same in every state: the reader must not see a flash of
    // "Sign in" on their way to the project list, or the reverse.
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Opening Halyard")
    expect(document.body.textContent ?? "").not.toMatch(/Email me a link|Start something/)
  })

  it("stays put and says what happened when the API cannot be reached", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("Failed to fetch")
      }),
    )
    renderWithQuery(<HomeRouter />)

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The request did not reach the server.")
    expect(alert).toHaveTextContent("Check your connection, then try again.")
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/signin")
    expect(router.replace).not.toHaveBeenCalled()
  })
})
