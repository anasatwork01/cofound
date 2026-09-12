import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { readSessionState } from "@/lib/session"

const SESSION = {
  user: { id: "u1", email: "founder@example.com", name: null },
  orgs: [],
}

function query(over: { isPending?: boolean; data?: typeof SESSION | undefined; error?: unknown }): {
  isPending: boolean
  data: typeof SESSION | undefined
  error: unknown
} {
  return {
    isPending: over.isPending ?? false,
    data: over.data,
    error: over.error ?? null,
  }
}

describe("readSessionState", () => {
  it("reports loading while the answer is still coming", () => {
    expect(readSessionState(query({ isPending: true }))).toEqual({ status: "loading" })
  })

  it("reads a 401 as signed out, because that is what the API means by it", () => {
    const state = readSessionState(
      query({
        error: new ApiError({
          status: 401,
          code: "unauthenticated",
          message: "You are not signed in.",
          fix: "Sign in and try again.",
          retriable: false,
        }),
      }),
    )
    expect(state).toEqual({ status: "signed-out" })
  })

  it("does NOT read a network failure as signed out", () => {
    // The distinction this whole module exists for. A console served from a CDN
    // while the API is down would otherwise tell a signed-in user they are
    // signed out, and send them to re-enter an email address at a screen whose
    // submit will fail the same way.
    const error = new TypeError("Failed to fetch")
    expect(readSessionState(query({ error }))).toEqual({ status: "unreachable", error })
  })

  it("does not read a 500 as signed out either", () => {
    const error = new ApiError({
      status: 503,
      code: "unavailable",
      message: "The server could not answer.",
      fix: "Try again in a moment.",
      retriable: true,
    })
    expect(readSessionState(query({ error })).status).toBe("unreachable")
  })

  it("reports signed in, with the session the API returned", () => {
    const state = readSessionState(query({ data: SESSION }))
    expect(state).toEqual({ status: "signed-in", session: SESSION })
  })

  it("prefers data it already has over a later failure", () => {
    // A background refetch that fails does not sign the reader out mid-task.
    const state = readSessionState(query({ data: SESSION, error: new TypeError("offline") }))
    expect(state.status).toBe("signed-in")
  })
})
