import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { SignInForm } from "@/app/signin/sign-in-form"
import { envelope, readableText, renderWithQuery, stubApi } from "@/test-utils"

afterEach(() => {
  cleanup()
})

function type(value: string) {
  fireEvent.change(screen.getByLabelText("Email address"), { target: { value } })
}

describe("SignInForm", () => {
  it("sends the address to POST /auth/magic-link", async () => {
    const { calls } = stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    type("founder@example.com")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    await waitFor(() => {
      expect(calls).toHaveLength(1)
    })
    expect(calls[0]?.method).toBe("POST")
    expect(calls[0]?.url).toContain("/auth/magic-link")
    expect(calls[0]?.body).toEqual({ email: "founder@example.com" })
  })

  it("will not submit an empty form", () => {
    const { calls } = stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    expect(calls).toHaveLength(0)
    expect(screen.getByText("Type the email address you want the link sent to.")).toBeVisible()
  })

  it("refuses an address the contract would refuse, without a round trip", () => {
    // The schema is the generated `RequestMagicLinkBodySchema`, so this is the
    // API's own rule enforced early rather than a second, drifting copy of it.
    const { calls } = stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    type("founder@")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    expect(calls).toHaveLength(0)
    expect(screen.getByText(/does not look like an email address/)).toBeVisible()
  })

  it("attaches the field error to the field, not just near it", () => {
    stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    type("founder@")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    const field = screen.getByLabelText("Email address")
    expect(field).toHaveAttribute("aria-invalid", "true")
    const describedBy = field.getAttribute("aria-describedby") ?? ""
    expect(document.getElementById(describedBy)?.textContent ?? "").toMatch(
      /does not look like an email address/,
    )
  })

  it("owns its validation rather than leaving it to the browser", () => {
    // `required` would make the browser fire a native bubble INSTEAD of submit,
    // so the handler never runs and the message above could never be shown —
    // two validation systems, one of them unstyleable and announced
    // inconsistently. `type=email` stays, for the keyboard it gives a phone.
    stubApi({ "/auth/magic-link": { status: 202 } })
    const { container } = renderWithQuery(<SignInForm />)
    expect(container.querySelector("form")).toHaveAttribute("noValidate")
    expect(screen.getByLabelText("Email address")).not.toHaveAttribute("required")
    expect(screen.getByLabelText("Email address")).toHaveAttribute("type", "email")
  })

  it("says a link was sent, and never that an account was found", async () => {
    // The endpoint answers 202 whether or not the address has an account, so
    // that it cannot be used to test whether a given person has one. A screen
    // that said "we found your account" — or "if you have an account" — would
    // give away exactly what the 202 is protecting, and would do it twice if
    // you compared the two wordings.
    stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    type("stranger@example.com")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    const sent = await screen.findByRole("heading", { name: "Check your email" })
    expect(sent).toBeInTheDocument()
    const text = readableText()
    expect(text).toContain("stranger@example.com")
    expect(text).not.toMatch(
      /\baccount (?:was )?found\b|\bwe found\b|\bif you have an account\b|\balready registered\b|\bexists\b|\bsigned up\b|\bno account\b/i,
    )
  })

  it("lets the reader correct the address they just sent to", async () => {
    stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)

    type("typo@exmaple.com")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))
    await screen.findByRole("heading", { name: "Check your email" })

    fireEvent.click(screen.getByRole("button", { name: "Use a different address" }))
    expect(screen.getByLabelText("Email address")).toBeInTheDocument()
  })

  it("keeps the action's name while it is happening", async () => {
    stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)
    type("founder@example.com")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))
    // SPEC §18: an action keeps its name through the whole flow. "Emailing a
    // link" is the same verb, not a different one.
    await waitFor(() => {
      expect(document.body.textContent ?? "").toMatch(/Emailing a link|Check your email/)
    })
  })

  it("surfaces the API's message and fix when the address is throttled", async () => {
    stubApi({
      "/auth/magic-link": {
        status: 429,
        body: envelope({
          code: "rate_limited",
          message: "Too many sign-in links have been requested for that address.",
          fix: "Check your inbox, or wait a few minutes and try again.",
          retriable: true,
        }),
      },
    })
    renderWithQuery(<SignInForm />)

    type("founder@example.com")
    fireEvent.click(screen.getByRole("button", { name: "Email me a link" }))

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("Too many sign-in links have been requested for that address.")
    expect(alert).toHaveTextContent("Check your inbox, or wait a few minutes and try again.")
    // Still on the form: a failure must not look like a success.
    expect(screen.queryByRole("heading", { name: "Check your email" })).toBeNull()
  })

  it("offers SPEC §8's other way in as a navigation, not a fetch", () => {
    stubApi({ "/auth/magic-link": { status: 202 } })
    renderWithQuery(<SignInForm />)
    const google = screen.getByRole("link", { name: "Continue with Google" })
    // `GET /auth/google/start` answers 303 to Google and sets the CSRF state
    // and PKCE verifier cookies on the way; neither survives being read by
    // `fetch`, so this has to be a real navigation.
    expect(google.getAttribute("href")).toMatch(/\/auth\/google\/start$/)
  })

  it("asks for no password, because SPEC §8 has none", () => {
    stubApi({ "/auth/magic-link": { status: 202 } })
    const { container } = renderWithQuery(<SignInForm />)
    expect(container.querySelector('input[type="password"]')).toBeNull()
  })
})
