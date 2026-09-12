import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }))
vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => "/new",
  useSearchParams: () => new URLSearchParams(),
}))

import { NewProjectPrompt, whyBlocked } from "@/app/new/new-project-prompt"
import { envelope, readableText, renderWithQuery, sessionBody, stubApi } from "@/test-utils"

const CREATED = {
  id: "44444444-4444-4444-8444-444444444444",
  org_id: "org-1",
  name: "Untitled project",
  slug: "untitled-project",
  default_branch: "main",
  git_authority: "internal",
  created_at: "2026-09-12T10:00:00Z",
}

function signedIn(
  projectResponse: { status?: number; body?: unknown } = { status: 201, body: CREATED },
) {
  return stubApi({
    "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
    "/projects": projectResponse,
  })
}

function describeIt(value: string) {
  fireEvent.change(screen.getByLabelText("What do you want to build?"), { target: { value } })
}

/**
 * Describe something and wait for the action to become available.
 *
 * The button is inert until the session says who you are and which org you are
 * in — that is the whole point of `whyBlocked` — so a test that clicks before
 * the session resolves is testing the inert path and reporting it as the live
 * one.
 */
async function describeAndStart(value: string) {
  describeIt(value)
  const start = screen.getByRole("button", { name: "Start building" })
  await waitFor(() => {
    expect(start).not.toHaveAttribute("aria-disabled")
  })
  fireEvent.click(start)
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  router.push.mockClear()
  window.localStorage.clear()
})

describe("whyBlocked", () => {
  it("names something the reader can do in every state that blocks", () => {
    const states = ["loading", "signed-out", "unreachable"] as const
    for (const status of states) {
      const reason = whyBlocked({ state: { status }, hasOrg: false, prompt: "x" })
      expect(reason, status).not.toBeNull()
      expect(reason ?? "").not.toMatch(/sorry|apolog|oops/i)
    }
    expect(whyBlocked({ state: { status: "signed-in" }, hasOrg: false, prompt: "x" })).toMatch(
      /organisation/i,
    )
    expect(whyBlocked({ state: { status: "signed-in" }, hasOrg: true, prompt: "   " })).toMatch(
      /Describe/,
    )
  })

  it("clears only when there is genuinely something to send", () => {
    expect(
      whyBlocked({ state: { status: "signed-in" }, hasOrg: true, prompt: "A shop" }),
    ).toBeNull()
  })
})

describe("NewProjectPrompt", () => {
  it("keeps what the reader types", () => {
    signedIn()
    renderWithQuery(<NewProjectPrompt />)
    describeIt("A booking page for my studio")
    expect(screen.getByLabelText("What do you want to build?")).toHaveValue(
      "A booking page for my studio",
    )
  })

  it("creates a project with the org in the header AND in the body", async () => {
    // Verified by hand against the running API: both are required, and they
    // must agree. The header selects the tenancy scope; the body says which org
    // the caller believes they are creating in, and the API refuses rather than
    // honouring the body — so a request cannot create a project in an org whose
    // role was never checked.
    const { calls } = signedIn()
    renderWithQuery(<NewProjectPrompt />)

    await describeAndStart("A booking page for my studio")

    await waitFor(() => {
      expect(calls.filter((call) => call.method === "POST")).toHaveLength(1)
    })
    const post = calls.find((call) => call.method === "POST")
    expect(post?.url).toContain("/projects")
    expect(post?.headers["x-halyard-org"]).toBe("acme")
    expect(post?.body).toEqual({
      org_id: "22222220-2222-4222-8222-222222222222",
      prompt: "A booking page for my studio",
    })
  })

  it("sends no name and no template, because §7.1 takes one or the other", async () => {
    const { calls } = signedIn()
    renderWithQuery(<NewProjectPrompt />)
    await describeAndStart("A shop")

    await waitFor(() => {
      expect(calls.some((call) => call.method === "POST")).toBe(true)
    })
    const body = calls.find((call) => call.method === "POST")?.body as Record<string, unknown>
    expect(Object.keys(body).sort()).toEqual(["org_id", "prompt"])
  })

  it("lands the reader in the builder for the project that was created", async () => {
    signedIn()
    renderWithQuery(<NewProjectPrompt />)
    await describeAndStart("A shop")

    await waitFor(() => {
      expect(router.push).toHaveBeenCalledWith("/p/untitled-project")
    })
  })

  it("invalidates the project list it just made stale", async () => {
    signedIn()
    const { queryClient } = renderWithQuery(<NewProjectPrompt />)
    const invalidate = vi.spyOn(queryClient, "invalidateQueries")

    await describeAndStart("A shop")

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ["projects", "acme"] })
    })
  })

  it("will not submit an empty prompt", async () => {
    const { calls } = signedIn()
    renderWithQuery(<NewProjectPrompt />)

    await waitFor(() => {
      expect(screen.getByText("Describe what you want built, then start.")).toBeVisible()
    })
    fireEvent.click(screen.getByRole("button", { name: "Start building" }))
    fireEvent.change(screen.getByLabelText("What do you want to build?"), {
      target: { value: "   " },
    })
    fireEvent.click(screen.getByRole("button", { name: "Start building" }))

    expect(calls.filter((call) => call.method === "POST")).toHaveLength(0)
  })

  it("never leaves an unavailable control without a reason a reader can reach", async () => {
    stubApi({
      "/auth/session": { status: 401, body: envelope({ code: "unauthenticated", message: "No." }) },
    })
    renderWithQuery(<NewProjectPrompt />)

    const start = screen.getByRole("button", { name: "Start building" })
    // `aria-disabled`, not `disabled`. A disabled button is not focusable, so
    // the reason below could never be announced to the keyboard or
    // screen-reader user it is written for: it would be attached to the one
    // element on the screen that cannot deliver it.
    expect(start).toHaveAttribute("aria-disabled", "true")
    expect(start).not.toBeDisabled()
    start.focus()
    expect(start).toHaveFocus()

    await waitFor(() => {
      const describedBy = start.getAttribute("aria-describedby") ?? ""
      expect(document.getElementById(describedBy)).toHaveTextContent("Sign in to start a project.")
    })
  })

  it("stops being unavailable once there is something to send", async () => {
    signedIn()
    renderWithQuery(<NewProjectPrompt />)
    const start = screen.getByRole("button", { name: "Start building" })
    await waitFor(() => {
      expect(start).toHaveAttribute("aria-disabled", "true")
    })
    describeIt("A shop")
    expect(start).not.toHaveAttribute("aria-disabled")
    expect(start).not.toHaveAttribute("aria-describedby")
  })

  it("tells a reader with no organisation what is missing", async () => {
    stubApi({ "/auth/session": { body: sessionBody([]) } })
    renderWithQuery(<NewProjectPrompt />)
    expect(
      await screen.findByText("Create an organisation first — projects belong to one."),
    ).toBeVisible()
  })

  it("surfaces the API's own message and fix when the create is refused", async () => {
    signedIn({
      status: 402,
      body: envelope({
        code: "payment_required",
        message: "This organisation has no build credits left.",
        fix: "Top up in Credits, then start again.",
      }),
    })
    renderWithQuery(<NewProjectPrompt />)

    await describeAndStart("A shop")

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("This organisation has no build credits left.")
    expect(alert).toHaveTextContent("Top up in Credits, then start again.")
    expect(router.push).not.toHaveBeenCalled()
    // What they typed survives the refusal.
    expect(screen.getByLabelText("What do you want to build?")).toHaveValue("A shop")
  })

  it("keeps the action's name while it is happening", async () => {
    signedIn()
    renderWithQuery(<NewProjectPrompt />)
    await describeAndStart("A shop")
    // SPEC §18: an action keeps its name through the whole flow. "Starting" is
    // the same verb, not "Create".
    await waitFor(() => {
      expect(readableText()).toMatch(/Starting|Start building/)
    })
    expect(readableText()).not.toMatch(/\bCreate\b/)
  })
})
