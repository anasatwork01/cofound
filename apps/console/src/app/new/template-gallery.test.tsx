import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TemplateGallery } from "@/app/new/template-gallery"
import { renderWithQuery } from "@/test-utils"

function template(id: string, imageReady: boolean) {
  return {
    id,
    display_name: id,
    summary: `Everything you need for ${id}.`,
    current_version: { id: `v-${id}`, version: "1.4.0", image_ready: imageReady },
  }
}

function respond(status: number, body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify(body), {
          status,
          headers: { "content-type": "application/json" },
        }),
    ),
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("TemplateGallery", () => {
  it("says it is loading, for people who cannot see the placeholders", () => {
    respond(200, { templates: [] })
    renderWithQuery(<TemplateGallery />)
    expect(screen.getByText("Loading templates")).toBeInTheDocument()
  })

  it("lists what the endpoint returned, with the version demoted to monospace", async () => {
    respond(200, { templates: [template("storefront", true)] })
    const { container } = renderWithQuery(<TemplateGallery />)

    expect(await screen.findByRole("heading", { name: "storefront" })).toBeInTheDocument()
    const version = screen.getByText("1.4.0")
    expect(version.className).toContain("font-mono")
    expect(version.className).toContain("text-xs")
    expect(container.querySelectorAll("li")).toHaveLength(1)
  })

  it("puts the templates that start quickly first, without naming the reason", async () => {
    respond(200, {
      templates: [template("slow-one", false), template("quick-one", true)],
    })
    renderWithQuery(<TemplateGallery />)

    await screen.findByRole("heading", { name: "quick-one" })
    const names = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)
    expect(names).toEqual(["quick-one", "slow-one"])
    expect(document.body.textContent).not.toMatch(/image/i)
  })

  it("invites a different route when there are no templates at all", async () => {
    respond(200, { templates: [] })
    renderWithQuery(<TemplateGallery />)
    expect(await screen.findByText(/describe what you want in the box above/i)).toBeInTheDocument()
  })

  it("says what happened and how to fix it, without apologising", async () => {
    respond(503, {
      error: {
        code: "unavailable",
        message: "Templates are not available.",
        fix: "Try again in a moment.",
        retriable: true,
        request_id: "req_42",
      },
    })
    renderWithQuery(<TemplateGallery />)

    const alert = await screen.findByRole("alert")
    // The server's own words, both halves. This used to read "The templates did
    // not load." over a hard-coded "Check your connection, then try again." —
    // the message thrown away, the fix replaced.
    expect(alert).toHaveTextContent("Templates are not available.")
    expect(alert).toHaveTextContent("Try again in a moment.")
    expect(alert.textContent ?? "").not.toMatch(/sorry|apolog|oops/i)
    // SPEC §18 gives each of the three states exactly one meaning, and a
    // failed request is not "waiting on you" — so the notice borrows none of
    // their colours and the copy carries it instead. See the comment in
    // template-gallery.tsx for the whole of the reasoning.
    for (const node of [alert, ...alert.querySelectorAll("*")]) {
      expect(String(node.className)).not.toMatch(/\b(?:bg|text|border)-(?:agent|waiting|live)\b/)
    }
    // The request id is present for a support conversation, and demoted.
    expect(screen.getByText("req_42").className).toContain("font-mono")
  })

  it("retries when asked, and shows what came back", async () => {
    respond(500, {})
    renderWithQuery(<TemplateGallery />)
    await screen.findByRole("alert")

    respond(200, { templates: [template("storefront", true)] })
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "storefront" })).toBeInTheDocument()
    })
  })
})

describe("the fix the gallery shows", () => {
  it("is the API's, even when the API's is nothing like a network problem", async () => {
    // The regression that prompted the shared `ErrorNotice`. A clean 401 says
    // "You are not signed in." / "Sign in and try again."; the gallery printed
    // "Check your connection, then try again." over the top of it and sent the
    // reader to look at their wifi.
    respond(401, {
      error: {
        code: "unauthenticated",
        message: "You are not signed in.",
        fix: "Sign in and try again.",
        retriable: false,
      },
    })
    renderWithQuery(<TemplateGallery />)

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("You are not signed in.")
    expect(alert).toHaveTextContent("Sign in and try again.")
    expect(alert.textContent ?? "").not.toMatch(/connection/i)
  })

  it("is the console's only when there was no response to quote", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("Failed to fetch")
      }),
    )
    renderWithQuery(<TemplateGallery />)

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("The request did not reach the server.")
    expect(alert).toHaveTextContent("Check your connection, then try again.")
  })
})
