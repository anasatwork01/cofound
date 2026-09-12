import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ErrorNotice } from "@/components/error-notice"
import { ApiError } from "@/lib/api"

afterEach(cleanup)

function apiError(over: Partial<ConstructorParameters<typeof ApiError>[0]> = {}): ApiError {
  return new ApiError({
    status: 401,
    code: "unauthenticated",
    message: "You are not signed in.",
    fix: "Sign in and try again.",
    retriable: false,
    ...over,
  })
}

describe("ErrorNotice", () => {
  it("puts the API's own message and the API's own fix on the screen", () => {
    render(<ErrorNotice error={apiError()} />)
    const alert = screen.getByRole("alert")
    expect(alert).toHaveTextContent("You are not signed in.")
    expect(alert).toHaveTextContent("Sign in and try again.")
  })

  it("does not substitute a generic fix for the one the server sent", () => {
    // The regression this component was extracted for. The template gallery
    // printed "Check your connection, then try again." over every failure,
    // including a clean 401 whose entire content is "sign in" — so the reader
    // was sent to look at their wifi by a screen that had been told exactly
    // what was wrong.
    render(<ErrorNotice error={apiError()} />)
    expect(screen.getByRole("alert").textContent ?? "").not.toMatch(/connection/i)
  })

  it("carries a 429's Retry-After wording through unchanged", () => {
    render(
      <ErrorNotice
        error={apiError({
          status: 429,
          code: "rate_limited",
          message: "Too many sign-in links have been requested for that address.",
          fix: "Check your inbox, or wait a few minutes and try again.",
          retriable: true,
        })}
      />,
    )
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Check your inbox, or wait a few minutes and try again.",
    )
  })

  it("owns the copy only when the request never reached a server", () => {
    // `fetch` rejects with a TypeError for DNS failure, offline, CORS and a
    // dropped connection alike. There is no envelope, so there is nothing to
    // surface, and this is the ONE branch allowed a sentence of its own.
    render(<ErrorNotice error={new TypeError("Failed to fetch")} />)
    const alert = screen.getByRole("alert")
    expect(alert).toHaveTextContent("The request did not reach the server.")
    expect(alert).toHaveTextContent("Check your connection, then try again.")
  })

  it("invents nothing when the envelope carries no fix", () => {
    const alert = render(<ErrorNotice error={apiError({ fix: undefined })} />)
    expect(screen.getByRole("alert")).toHaveTextContent("You are not signed in.")
    // A made-up next step is the failure this component exists to prevent, so
    // the second paragraph is absent rather than filled.
    expect(alert.container.querySelectorAll("p")).toHaveLength(1)
  })

  it("never apologises", () => {
    render(<ErrorNotice error={apiError()} />)
    expect(screen.getByRole("alert").textContent ?? "").not.toMatch(
      /sorry|apolog|oops|unfortunately/i,
    )
  })

  it("borrows none of SPEC §18's three state colours", () => {
    // §18 fixes amber as "waiting on you" and requires it to mean that every
    // time. A failed request is not a state the product is correctly in, so
    // painting it amber would quietly redefine the state to "waiting on you, or
    // broken" — and then the three stop being one learned pattern.
    const { container } = render(
      <ErrorNotice error={apiError()} action={{ label: "Try again", onClick: vi.fn() }} />,
    )
    for (const node of container.querySelectorAll("*")) {
      expect(String(node.className)).not.toMatch(/\b(?:bg|text|border)-(?:agent|waiting|live)\b/)
    }
  })

  it("announces itself, because a notice nobody hears is not one", () => {
    render(<ErrorNotice error={apiError()} />)
    expect(screen.getByRole("alert")).toBeInTheDocument()
  })

  it("runs the retry it was given", () => {
    const onClick = vi.fn()
    render(<ErrorNotice error={apiError()} action={{ label: "Try again", onClick }} />)
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it("shows the request id for a support conversation, demoted", () => {
    render(<ErrorNotice error={apiError({ requestId: "req_42" })} />)
    expect(screen.getByText("req_42").className).toContain("font-mono")
    expect(screen.getByText("req_42").className).toContain("text-xs")
  })
})
