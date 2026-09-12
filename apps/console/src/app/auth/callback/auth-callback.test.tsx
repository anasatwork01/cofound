import { StrictMode } from "react"
import { QueryClient } from "@tanstack/react-query"
import { act, cleanup, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const search = vi.hoisted(() => ({ current: "" }))
const router = vi.hoisted(() => ({ replace: vi.fn(), push: vi.fn() }))
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(search.current),
  useRouter: () => router,
  usePathname: () => "/auth/callback",
}))

import { AuthCallback } from "@/app/auth/callback/auth-callback"
import { envelope, renderWithQuery, sessionBody, stubApi } from "@/test-utils"

afterEach(() => {
  cleanup()
  router.replace.mockClear()
  router.push.mockClear()
})

describe("the emailed link", () => {
  it("exchanges the token the API put in the link", async () => {
    search.current = `token=${"t".repeat(43)}`
    const { calls } = stubApi({
      "/auth/magic-link/verify": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
    })
    renderWithQuery(<AuthCallback />)

    await waitFor(() => {
      expect(calls).toHaveLength(1)
    })
    expect(calls[0]?.url).toContain("/auth/magic-link/verify")
    expect(calls[0]?.body).toEqual({ token: "t".repeat(43) })
  })

  it("exchanges once per arrival, whatever makes it render again", async () => {
    // A link token is single-use: the second exchange of the same token is
    // answered exactly like an unknown one. So an effect that can re-fire turns
    // a working link into "that sign-in link is no longer valid" on the
    // reader's first try — and the thing that re-fires it is not a re-render,
    // it is a CHANGE in what the effect reads. A client-side navigation from
    // one `?token=` to another is that change, and it must not spend the second
    // token on its own.
    search.current = `token=${"a".repeat(43)}`
    const { calls } = stubApi({
      "/auth/magic-link/verify": { body: sessionBody([]) },
    })
    const { rerender } = renderWithQuery(
      // StrictMode as well, because `next dev` runs it: React mounts, unmounts
      // and remounts every effect there.
      <StrictMode>
        <AuthCallback />
      </StrictMode>,
    )
    await waitFor(() => {
      expect(calls.length).toBeGreaterThan(0)
    })

    search.current = `token=${"b".repeat(43)}`
    rerender(
      <StrictMode>
        <AuthCallback />
      </StrictMode>,
    )

    // Flushed before asserting. `mutate` returns before its `mutationFn` has
    // called `fetch`, so an assertion made immediately after the re-render
    // counts the requests of a component that has not made its second one yet —
    // and passes whether or not the guard is there. Measured: without this the
    // whole test went green with the guard deleted.
    await act(async () => {
      await new Promise((resolve) => {
        setTimeout(resolve, 0)
      })
    })

    expect(calls).toHaveLength(1)
    expect(calls[0]?.body).toEqual({ token: "a".repeat(43) })
  })

  it("seeds the session it was handed rather than asking again", async () => {
    search.current = `token=${"t".repeat(43)}`
    const { calls } = stubApi({
      "/auth/magic-link/verify": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
    })
    // A client that keeps what it is given: the default harness collects a
    // query the moment nothing observes it, and the point of this test is that
    // the session was written down rather than fetched again.
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    const { queryClient } = renderWithQuery(<AuthCallback />, client)

    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/")
    })
    expect(queryClient.getQueryData(["session"])).toMatchObject({
      orgs: [{ slug: "acme" }],
    })
    // One request: the response IS the session, so a second GET would be the
    // same answer bought twice.
    expect(calls).toHaveLength(1)
  })

  it("says what happened and how to fix it when the link is spent", async () => {
    search.current = `token=${"x".repeat(43)}`
    stubApi({
      "/auth/magic-link/verify": {
        status: 401,
        body: envelope({
          code: "unauthenticated",
          message: "That sign-in link is no longer valid.",
          fix: "Request a new one.",
        }),
      },
    })
    renderWithQuery(<AuthCallback />)

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("That sign-in link is no longer valid.")
    expect(alert).toHaveTextContent("Request a new one.")
    expect(screen.getByRole("link", { name: "Back to sign in" })).toHaveAttribute("href", "/signin")
    expect(router.replace).not.toHaveBeenCalled()
  })
})

describe("the Google return", () => {
  it("moves on when Google succeeded, because the cookie is already set", async () => {
    search.current = ""
    stubApi({})
    renderWithQuery(<AuthCallback />)
    await waitFor(() => {
      expect(router.replace).toHaveBeenCalledWith("/")
    })
  })

  it.each([
    ["declined", "You did not finish signing in with Google."],
    ["expired", "That sign-in attempt took too long."],
    ["state_mismatch", "That sign-in could not be verified."],
    ["exchange_failed", "Google did not complete the sign-in."],
    ["email_unverified", "Google has not verified that email address."],
  ])("gives %s a sentence of its own", (reason, expected) => {
    // The API sends "a stable code, never a message", precisely so a provider's
    // error string never reaches a page — so the console owns this wording, and
    // every code the API can emit has to have some.
    search.current = `error=${reason}`
    stubApi({})
    renderWithQuery(<AuthCallback />)
    expect(screen.getByRole("heading", { name: expected })).toBeInTheDocument()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it("still says something useful for a code it has never seen", () => {
    search.current = "error=something_new"
    stubApi({})
    renderWithQuery(<AuthCallback />)
    expect(screen.getByRole("heading", { name: "That sign-in did not complete." })).toBeVisible()
    expect(screen.getByRole("link", { name: "Back to sign in" })).toBeInTheDocument()
  })

  it("never apologises, whatever went wrong", () => {
    for (const reason of ["declined", "expired", "state_mismatch", "exchange_failed", "nonsense"]) {
      cleanup()
      search.current = `error=${reason}`
      stubApi({})
      renderWithQuery(<AuthCallback />)
      expect(document.body.textContent ?? "").not.toMatch(/sorry|apolog|oops|unfortunately/i)
    }
  })

  it("makes no request at all on an error return", () => {
    search.current = "error=declined"
    const { calls } = stubApi({})
    renderWithQuery(<AuthCallback />)
    expect(calls).toHaveLength(0)
  })
})
