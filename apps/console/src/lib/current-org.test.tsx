import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { CURRENT_ORG_KEY, rememberOrg, resolveCurrentOrg, useCurrentOrg } from "@/lib/current-org"
import type { AuthOrgMembership } from "@/lib/api"

function org(slug: string): AuthOrgMembership {
  return { id: `id-${slug}`, slug, name: slug.toUpperCase(), role: "owner" }
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("resolveCurrentOrg", () => {
  it("uses the remembered org when the user is still in it", () => {
    expect(resolveCurrentOrg([org("acme"), org("beta")], "beta")?.slug).toBe("beta")
  })

  it("falls back to the first membership when the remembered one is gone", () => {
    // The stored slug is a HINT, never an authority: a user removed from an org
    // must not be stuck sending its slug on every request and being refused.
    expect(resolveCurrentOrg([org("acme")], "beta")?.slug).toBe("acme")
  })

  it("resolves to nothing when the user is in no org at all", () => {
    expect(resolveCurrentOrg([], "beta")).toBeNull()
    expect(resolveCurrentOrg([], null)).toBeNull()
  })

  it("never invents an org the session did not return", () => {
    // Everything downstream sends this slug as `X-Halyard-Org`. A slug that came
    // from storage rather than from the session would be the console choosing a
    // tenant for itself.
    window.localStorage.setItem(CURRENT_ORG_KEY, "someone-elses-org")
    const resolved = resolveCurrentOrg([org("acme")], window.localStorage.getItem(CURRENT_ORG_KEY))
    expect(resolved?.slug).toBe("acme")
  })
})

function Probe({ orgs }: { orgs: readonly AuthOrgMembership[] }) {
  const { current, select } = useCurrentOrg(orgs)
  return (
    <div>
      <p data-testid="current">{current?.slug ?? "none"}</p>
      <button type="button" onClick={() => select("beta")}>
        Switch
      </button>
    </div>
  )
}

describe("useCurrentOrg", () => {
  it("starts on the first membership when nothing is remembered", () => {
    render(<Probe orgs={[org("acme"), org("beta")]} />)
    expect(screen.getByTestId("current")).toHaveTextContent("acme")
  })

  it("re-renders on a switch, and remembers it for next time", () => {
    render(<Probe orgs={[org("acme"), org("beta")]} />)
    fireEvent.click(screen.getByRole("button", { name: "Switch" }))
    expect(screen.getByTestId("current")).toHaveTextContent("beta")
    expect(window.localStorage.getItem(CURRENT_ORG_KEY)).toBe("beta")
  })

  it("reads what a previous visit stored", () => {
    rememberOrg("beta")
    render(<Probe orgs={[org("acme"), org("beta")]} />)
    expect(screen.getByTestId("current")).toHaveTextContent("beta")
  })

  it("renders rather than throwing when storage is blocked", () => {
    // Safari's private mode and a blocked-storage setting THROW from
    // `localStorage`, they do not return null. An org switcher that took the
    // page down with it would be a worse bug than a preference that does not
    // persist.
    const original = Object.getOwnPropertyDescriptor(window, "localStorage")
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      get() {
        throw new Error("The operation is insecure.")
      },
    })
    try {
      render(<Probe orgs={[org("acme")]} />)
      expect(screen.getByTestId("current")).toHaveTextContent("acme")
      fireEvent.click(screen.getByRole("button", { name: "Switch" }))
    } finally {
      if (original !== undefined) Object.defineProperty(window, "localStorage", original)
    }
  })
})
