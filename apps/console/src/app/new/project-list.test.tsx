import { cleanup, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/new",
  useSearchParams: () => new URLSearchParams(),
}))

import { ProjectList } from "@/app/new/project-list"
import { CURRENT_ORG_KEY } from "@/lib/current-org"
import { envelope, renderWithQuery, sessionBody, stubApi } from "@/test-utils"

function project(over: { id: string; name: string; slug: string; created_at?: string }) {
  return {
    id: over.id,
    org_id: "org-1",
    name: over.name,
    slug: over.slug,
    default_branch: "main",
    git_authority: "internal",
    created_at: over.created_at ?? "2026-09-01T10:00:00Z",
  }
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("ProjectList", () => {
  it("sends X-Halyard-Org, because without it the API refuses to guess", async () => {
    // Measured against the running API: no header, and `GET /v1/projects`
    // answers "That request needs an organisation." A project slug is unique
    // only WITHIN an org (SPEC §6), so serving a guess would mean reading the
    // wrong tenant's list.
    const { calls } = stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    renderWithQuery(<ProjectList />)

    await waitFor(() => {
      expect(calls.filter((call) => call.url.includes("/projects"))).toHaveLength(1)
    })
    const listed = calls.find((call) => call.url.includes("/projects"))
    expect(listed?.headers["x-halyard-org"]).toBe("acme")
  })

  it("asks for nothing at all before it knows which org", async () => {
    const { calls } = stubApi({
      "/auth/session": { status: 401, body: envelope({ code: "unauthenticated", message: "No." }) },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    renderWithQuery(<ProjectList />)

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes("/auth/session"))).toBe(true)
    })
    // A request with no org header would be refused; one with a guessed org
    // would be worse.
    expect(calls.filter((call) => call.url.includes("/projects"))).toHaveLength(0)
  })

  it("uses the org the reader last chose, not always the first", async () => {
    window.localStorage.setItem(CURRENT_ORG_KEY, "beta-works")
    const { calls } = stubApi({
      "/auth/session": {
        body: sessionBody([
          { slug: "acme", name: "Acme" },
          { slug: "beta-works", name: "Beta Works" },
        ]),
      },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    renderWithQuery(<ProjectList />)

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes("/projects"))).toBe(true)
    })
    expect(calls.find((call) => call.url.includes("/projects"))?.headers["x-halyard-org"]).toBe(
      "beta-works",
    )
  })

  it("invites the reader to start one when the list is empty", async () => {
    // SPEC §18: empty states are invitations to act, not reports that something
    // is empty. The invitation is the prompt box above, so this points at it.
    stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    renderWithQuery(<ProjectList />)

    expect(await screen.findByRole("heading", { name: "No projects yet" })).toBeVisible()
    expect(document.body.textContent ?? "").toMatch(/Describe what you want built in the box above/)
    expect(document.body.textContent ?? "").not.toMatch(/no data|nothing here|empty/i)
  })

  it("lists what came back, newest first, each linking to its builder", async () => {
    stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
      "/projects": {
        body: {
          projects: [
            project({
              id: "1",
              name: "Old thing",
              slug: "old-thing",
              created_at: "2026-01-01T00:00:00Z",
            }),
            project({
              id: "2",
              name: "New thing",
              slug: "new-thing",
              created_at: "2026-09-01T00:00:00Z",
            }),
          ],
          page: { has_more: false },
        },
      },
    })
    renderWithQuery(<ProjectList />)

    const links = await screen.findAllByRole("link")
    expect(links.map((link) => link.textContent)).toEqual([
      "New thingnew-thing",
      "Old thingold-thing",
    ])
    expect(links[0]).toHaveAttribute("href", "/p/new-thing")
  })

  it("points at org creation when the reader is in none", async () => {
    stubApi({
      "/auth/session": { body: sessionBody([]) },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    renderWithQuery(<ProjectList />)

    expect(await screen.findByRole("heading", { name: "No organisation yet" })).toBeVisible()
    expect(screen.getByRole("link", { name: "Create an organisation" })).toHaveAttribute(
      "href",
      "/orgs/new",
    )
  })

  it("says nothing at all to a signed-out reader", async () => {
    // The prompt box above already says "Sign in to start a project". A second
    // copy is noise, and a "Your projects" heading over an empty box would tell
    // a signed-out reader they have none.
    stubApi({
      "/auth/session": { status: 401, body: envelope({ code: "unauthenticated", message: "No." }) },
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    const { container } = renderWithQuery(<ProjectList />)
    await waitFor(() => {
      expect(container.textContent).toBe("")
    })
  })

  it("surfaces the API's own message and fix when the list fails", async () => {
    stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
      "/projects": {
        status: 403,
        body: envelope({
          code: "forbidden",
          message: "Your role does not allow you to list projects.",
          fix: "Ask an owner or admin of this organisation to change your role.",
        }),
      },
    })
    renderWithQuery(<ProjectList />)

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("Your role does not allow you to list projects.")
    expect(alert).toHaveTextContent(
      "Ask an owner or admin of this organisation to change your role.",
    )
  })

  it("announces the wait for people who cannot see the placeholders", async () => {
    // The skeleton blocks are decoration: `aria-hidden` in effect, because they
    // are empty divs. Without the sr-only line, a screen-reader user meets a
    // heading with nothing under it and no way to know more is coming.
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: unknown) => {
        if (String(input).includes("/auth/session")) {
          return new Response(JSON.stringify(sessionBody([{ slug: "acme", name: "Acme" }])), {
            status: 200,
            headers: { "content-type": "application/json" },
          })
        }
        // Never resolves: the loading state is the thing under test, and a
        // stub that answered would make this a race with the assertion.
        return new Promise<Response>(() => {})
      }),
    )
    const { container } = renderWithQuery(<ProjectList />)

    expect(await screen.findByText("Loading your projects")).toBeInTheDocument()
    expect(container.querySelector("section")).toHaveAttribute("aria-busy", "true")
  })
})
