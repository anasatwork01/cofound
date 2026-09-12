import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }))
vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => "/orgs/new",
  useSearchParams: () => new URLSearchParams(),
}))

import { CreateOrgForm } from "@/app/orgs/new/create-org-form"
import { CURRENT_ORG_KEY } from "@/lib/current-org"
import { envelope, renderWithQuery, stubApi } from "@/test-utils"

const CREATED = {
  id: "33333333-3333-4333-8333-333333333333",
  name: "Acme",
  slug: "acme",
  plan: "free",
  created_at: "2026-09-12T10:00:00Z",
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  router.push.mockClear()
  window.localStorage.clear()
})

function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label, { exact: false }), { target: { value } })
}

describe("CreateOrgForm", () => {
  it("posts the name to /orgs", async () => {
    const { calls } = stubApi({
      "/orgs": { status: 201, body: CREATED },
      "/auth/session": { body: { user: { id: "u1", email: "a@b.c" }, orgs: [] } },
    })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    await waitFor(() => {
      expect(calls.filter((call) => call.url.includes("/orgs"))).toHaveLength(1)
    })
    const post = calls[0]
    expect(post?.method).toBe("POST")
    // No `slug` key at all, rather than `slug: undefined`: the API derives one
    // from the name, and a present-but-empty slug is a different request.
    expect(post?.body).toEqual({ name: "Acme" })
  })

  it("sends the short name when one is given, so the API's fix is reachable", async () => {
    // The API answers a name collision with "Pick a different name, or set a
    // slug explicitly." Half of that fix is unreachable from a form with no
    // second field, and SPEC §18 requires a fix the reader can act on.
    const { calls } = stubApi({
      "/orgs": { status: 201, body: CREATED },
      "/auth/session": { body: { user: { id: "u1", email: "a@b.c" }, orgs: [] } },
    })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fill("Short name", "acme-two")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    await waitFor(() => {
      expect(calls.length).toBeGreaterThan(0)
    })
    expect(calls[0]?.body).toEqual({ name: "Acme", slug: "acme-two" })
  })

  it("will not submit an empty form", () => {
    const { calls } = stubApi({ "/orgs": { status: 201, body: CREATED } })
    renderWithQuery(<CreateOrgForm />)

    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    expect(calls).toHaveLength(0)
    expect(screen.getByText(/Give the organisation a name/)).toBeVisible()
  })

  it("refuses a short name the contract would refuse, and says which field", () => {
    // `SlugSchema` is generated from `common.schema.json`: lowercase, DNS-label
    // safe, because a slug reaches a hostname (SPEC §9).
    const { calls } = stubApi({ "/orgs": { status: 201, body: CREATED } })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fill("Short name", "Acme Inc!")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    expect(calls).toHaveLength(0)
    const field = screen.getByLabelText("Short name", { exact: false })
    expect(field).toHaveAttribute("aria-invalid", "true")
    expect(screen.getByText(/lowercase letters, numbers and hyphens/)).toBeVisible()
  })

  it("invalidates the session, because creating an org changes who you are", async () => {
    // The API makes the caller the org's owner in the same transaction, so the
    // session's `orgs` list is stale the instant this resolves — and `orgs` is
    // what every screen reads to decide which org it is acting as.
    const { calls } = stubApi({
      "/orgs": { status: 201, body: CREATED },
      "/auth/session": { body: { user: { id: "u1", email: "a@b.c" }, orgs: [] } },
    })
    const { queryClient } = renderWithQuery(<CreateOrgForm />)
    const invalidate = vi.spyOn(queryClient, "invalidateQueries")

    fill("Organisation name", "Acme")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ["session"] })
    })
    expect(calls.length).toBeGreaterThan(0)
  })

  it("lands you on the new org, and remembers it is the one you are in", async () => {
    stubApi({
      "/orgs": { status: 201, body: CREATED },
      "/auth/session": { body: { user: { id: "u1", email: "a@b.c" }, orgs: [] } },
    })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    await waitFor(() => {
      expect(router.push).toHaveBeenCalledWith("/new")
    })
    expect(window.localStorage.getItem(CURRENT_ORG_KEY)).toBe("acme")
  })

  it("surfaces the API's own message and fix on a name collision", async () => {
    stubApi({
      "/orgs": {
        status: 409,
        body: envelope({
          code: "conflict",
          message: "That organisation name is already taken.",
          fix: "Pick a different name, or set a slug explicitly.",
        }),
      },
    })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("That organisation name is already taken.")
    expect(alert).toHaveTextContent("Pick a different name, or set a slug explicitly.")
    expect(router.push).not.toHaveBeenCalled()
  })

  it("gives a signed-out reader the API's 401 and the link its fix names", async () => {
    // Not gated on the session by design: the only authority on whether you may
    // create an org is the API, and a client-side guess would put a spinner in
    // front of the form for everyone and flash the wrong screen when it guessed
    // wrong. So the refusal is the API's, in the API's words.
    stubApi({
      "/orgs": {
        status: 401,
        body: envelope({
          code: "unauthenticated",
          message: "You are not signed in.",
          fix: "Sign in and try again.",
        }),
      },
    })
    renderWithQuery(<CreateOrgForm />)

    fill("Organisation name", "Acme")
    fireEvent.click(screen.getByRole("button", { name: "Create organisation" }))

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("You are not signed in.")
    expect(alert).toHaveTextContent("Sign in and try again.")
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/signin")
    // What they typed is still there to send again.
    expect(screen.getByLabelText("Organisation name")).toHaveValue("Acme")
  })

  it("has a real label on every field, never a placeholder", () => {
    stubApi({ "/orgs": { status: 201, body: CREATED } })
    const { container } = renderWithQuery(<CreateOrgForm />)
    for (const input of container.querySelectorAll("input")) {
      expect(input.id).not.toBe("")
      expect(container.querySelector(`label[for="${input.id}"]`)).not.toBeNull()
      expect(input.getAttribute("placeholder")).toBeNull()
    }
  })
})
