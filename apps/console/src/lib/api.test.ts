import { readFileSync } from "node:fs"
import { afterEach, describe, expect, it, vi } from "vitest"
import {
  ApiError,
  createOrg,
  createProject,
  fetchProjects,
  fetchSession,
  fetchTemplates,
  requestMagicLink,
  shouldRetry,
  signOut,
  verifyMagicLink,
} from "@/lib/api"
import { envelope, sessionBody, stubApi } from "@/test-api"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("fetchTemplates", () => {
  it("returns the documented envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(200, { templates: [] })),
    )
    await expect(fetchTemplates()).resolves.toEqual({ templates: [] })
  })

  it("sends the session cookie, because the API is on another origin", async () => {
    let sent: RequestInit | undefined
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        sent = init
        return jsonResponse(200, { templates: [] })
      }),
    )
    await fetchTemplates()
    expect(sent?.credentials).toBe("include")
  })

  it("raises the server's own message and fix", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse(409, {
          error: {
            code: "ambiguous_project",
            message: "Two of your organisations have a project with that name.",
            fix: "Switch organisation in the picker, then open it again.",
            retriable: false,
            request_id: "req_1",
          },
        }),
      ),
    )
    const error = await fetchTemplates().catch((caught: unknown) => caught)
    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.code).toBe("ambiguous_project")
    expect(apiError.fix).toBe("Switch organisation in the picker, then open it again.")
    expect(apiError.requestId).toBe("req_1")
    expect(apiError.retriable).toBe(false)
  })

  it("still says what happened and how to fix it when the body is not an envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("<html>502</html>", { status: 502 })),
    )
    const error = (await fetchTemplates().catch((caught: unknown) => caught)) as ApiError
    expect(error.message).toBe("The server could not answer.")
    expect(error.fix).toBe("Try again in a moment.")
    expect(error.retriable).toBe(true)
  })

  // NOT fetchTemplates: 404 is the one status it deliberately does not raise.
  // See the test below, and the comment on fetchTemplates.
  it("never apologises", async () => {
    for (const status of [401, 403, 404, 429, 500, 418]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => new Response("", { status })),
      )
      const error = (await fetchProjects("acme").catch((caught: unknown) => caught)) as ApiError
      expect(`${error.message} ${error.fix ?? ""}`.toLowerCase()).not.toMatch(
        /sorry|apolog|oops|unfortunately/,
      )
      expect(error.fix).toBeTruthy()
    }
  })

  // The template registry is task 2.1. Until it ships the API mounts no
  // `/v1/templates` route, so the collection answers 404 for the same reason it
  // would answer an empty list — there are none. The gallery's empty state says
  // something true and useful; its error state offers a "Try again" that cannot
  // ever succeed, on the console's primary screen.
  it("reads a missing templates route as an empty gallery", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 404 })),
    )
    await expect(fetchTemplates()).resolves.toEqual({ templates: [] })
  })

  // Narrow on purpose. If 404 were widened to "any failure is an empty
  // gallery", a signed-out console would show "no templates are installed"
  // instead of sending the reader to sign in.
  it("still raises every other failure", async () => {
    for (const status of [401, 403, 429, 500]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => new Response("", { status })),
      )
      const error = (await fetchTemplates().catch((caught: unknown) => caught)) as ApiError
      expect(error).toBeInstanceOf(ApiError)
      expect(error.status).toBe(status)
    }
  })
})

describe("shouldRetry", () => {
  it("retries what never reached a server", () => {
    expect(shouldRetry(0, new TypeError("Failed to fetch"))).toBe(true)
  })

  it("does not retry a refusal the server described as final", () => {
    const error = new ApiError({
      status: 422,
      code: "invalid",
      message: "That name is already taken.",
      retriable: false,
    })
    expect(shouldRetry(0, error)).toBe(false)
  })

  it("retries what the server said was worth retrying", () => {
    const error = new ApiError({
      status: 503,
      code: "unavailable",
      message: "The server could not answer.",
      retriable: true,
    })
    expect(shouldRetry(0, error)).toBe(true)
    expect(shouldRetry(2, error)).toBe(false)
  })
})

describe("the org header", () => {
  it("is on every project call, because the API refuses to guess without it", async () => {
    const { calls } = stubApi({
      "/projects": { body: { projects: [], page: { has_more: false } } },
    })
    await fetchProjects("acme")
    expect(calls).toHaveLength(1)
    // Lower-cased by the stub; HTTP header names are case-insensitive.
    expect(calls[0]?.headers["x-halyard-org"]).toBe("acme")
  })

  it("is NOT sent on a call that names no organisation", async () => {
    // `GET /auth/session` is answered before the caller is in any org, and
    // `POST /orgs` creates the one they will be in. Sending a header naming an
    // org neither request is about is how a console starts scoping a request to
    // the wrong tenant.
    const { calls } = stubApi({
      "/auth/session": { body: sessionBody() },
      "/orgs": { status: 201, body: { id: "o1", name: "Acme", slug: "acme" } },
    })
    await fetchSession()
    await createOrg({ name: "Acme" })
    for (const call of calls) expect(call.headers["x-halyard-org"]).toBeUndefined()
  })

  it("carries the org twice on a create, because the API checks they agree", async () => {
    const { calls } = stubApi({ "/projects": { status: 201, body: { id: "p1", slug: "shop" } } })
    await createProject({ org_id: "org-uuid", prompt: "A booking page" }, "acme")

    // The header selects the tenancy scope the request runs in; the body says
    // which org the caller believes they are creating in. The API answers
    // not-found when they disagree rather than honouring the body, so a request
    // cannot create a project in an org whose role was never checked.
    expect(calls[0]?.headers["x-halyard-org"]).toBe("acme")
    expect(calls[0]?.body).toEqual({ org_id: "org-uuid", prompt: "A booking page" })
    expect(calls[0]?.method).toBe("POST")
  })
})

describe("mutations", () => {
  it("asks for a magic link and resolves on the 202, which carries no body", async () => {
    const { calls } = stubApi({ "/auth/magic-link": { status: 202 } })
    await expect(requestMagicLink({ email: "founder@example.com" })).resolves.toBeUndefined()
    expect(calls[0]?.method).toBe("POST")
    expect(calls[0]?.body).toEqual({ email: "founder@example.com" })
    expect(calls[0]?.headers["content-type"]).toBe("application/json")
  })

  it("does not read a body off the 202, because reading one throws", async () => {
    // The endpoint answers 202 with nothing at all — deliberately, so the
    // response cannot differ for an address that has an account. A client that
    // called `response.json()` here would reject on every successful request.
    stubApi({ "/auth/magic-link": { status: 202 } })
    await expect(requestMagicLink({ email: "nobody@example.com" })).resolves.toBeUndefined()
  })

  it("exchanges a link token for the session it returns", async () => {
    const { calls } = stubApi({
      "/auth/magic-link/verify": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
    })
    const session = await verifyMagicLink({ token: "t".repeat(43) })
    expect(session.orgs[0]?.slug).toBe("acme")
    expect(calls[0]?.url).toContain("/auth/magic-link/verify")
  })

  it("signs out with a DELETE that carries no body and returns nothing", async () => {
    const { calls } = stubApi({ "/auth/session": { status: 204 } })
    await expect(signOut()).resolves.toBeUndefined()
    expect(calls[0]?.method).toBe("DELETE")
    expect(calls[0]?.body).toBeUndefined()
    expect(calls[0]?.headers["content-type"]).toBeUndefined()
  })

  it("raises the server's own message and fix when a create is refused", async () => {
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
    const error = (await createOrg({ name: "Acme" }).catch((caught: unknown) => caught)) as ApiError
    expect(error).toBeInstanceOf(ApiError)
    expect(error.message).toBe("That organisation name is already taken.")
    expect(error.fix).toBe("Pick a different name, or set a slug explicitly.")
  })

  it("sends no Idempotency-Key, which is a decision rather than an omission", () => {
    // SPEC §7.1's header makes a RETRY safe. This console does not retry
    // mutations (`providers.tsx`), so a fresh key per attempt would buy nothing
    // — and a key held across attempts is worse: the API fingerprints the body,
    // so a user who corrects a field the server rejected and submits again is
    // answered "That Idempotency-Key was already used for a different request."
    // If something here starts retrying, it mints a key for that body and this
    // assertion changes with it.
    const source = readFileSync(new URL("./api.ts", import.meta.url), "utf8")
    expect(source).not.toMatch(/headers\[["']Idempotency-Key["']\]|"Idempotency-Key":/i)
  })
})

describe("the session cookie", () => {
  it("is sent on a mutation as well as on a read", async () => {
    // SPEC §8 puts the console's session in an httpOnly cookie and the API is
    // on another origin, so it travels only when asked for explicitly. A read
    // that includes it and a write that does not is a console where everything
    // looks signed in until you try to do something.
    const seen: Array<RequestCredentials | undefined> = []
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: unknown, init?: RequestInit) => {
        seen.push(init?.credentials)
        return new Response(null, { status: 202 })
      }),
    )
    await requestMagicLink({ email: "founder@example.com" })
    expect(seen).toEqual(["include"])
  })
})
