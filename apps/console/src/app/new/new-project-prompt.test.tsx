import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NewProjectPrompt } from "@/app/new/new-project-prompt"
import { renderWithQuery } from "@/test-utils"

function session(status: number) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify(
            status === 200
              ? { user: { id: "u1", email: "a@b.c" }, orgs: [] }
              : { error: { code: "unauthenticated", message: "No.", retriable: false } },
          ),
          { status, headers: { "content-type": "application/json" } },
        ),
    ),
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("NewProjectPrompt", () => {
  it("asks the question in the reader's language and keeps what they type", () => {
    session(200)
    renderWithQuery(<NewProjectPrompt />)

    const box = screen.getByLabelText("What do you want to build?")
    fireEvent.change(box, { target: { value: "A booking page for my studio" } })
    expect(box).toHaveValue("A booking page for my studio")
  })

  it("never leaves an unavailable control without a reason a reader can reach", async () => {
    session(200)
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

    const describedBy = start.getAttribute("aria-describedby")
    expect(describedBy).toBeTruthy()
    const reason = document.getElementById(describedBy ?? "")
    await waitFor(() => {
      expect(reason).toHaveTextContent("The builder is not connected yet")
    })
  })

  it("is inert when the unavailable action is pressed", async () => {
    session(200)
    const { container } = renderWithQuery(<NewProjectPrompt />)

    const box = screen.getByLabelText("What do you want to build?")
    fireEvent.change(box, { target: { value: "A booking page" } })
    await waitFor(() => {
      expect(screen.getByText(/builder is not connected yet/)).toBeInTheDocument()
    })

    const form = container.querySelector("form")
    expect(form).not.toBeNull()
    fireEvent.submit(form as HTMLFormElement)
    fireEvent.click(screen.getByRole("button", { name: "Start building" }))

    // Reachable, and still nothing sent: `GET /auth/session` is the only
    // request this screen makes until `POST /projects` exists.
    const calls = vi.mocked(fetch).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    for (const [url, init] of calls) {
      expect(String(url)).toContain("/auth/session")
      expect((init as RequestInit | undefined)?.method ?? "GET").toBe("GET")
    }
    expect(box).toHaveValue("A booking page")
  })

  it("tells a signed-out reader the one thing they can do about it", async () => {
    session(401)
    renderWithQuery(<NewProjectPrompt />)
    expect(await screen.findByText("Sign in to start a project.")).toBeInTheDocument()
  })
})
