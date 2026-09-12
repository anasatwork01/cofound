import { afterEach, describe, expect, it, vi } from "vitest"
import { envelope, sessionBody, stubApi } from "@/test-api"

afterEach(() => {
  vi.unstubAllGlobals()
})

/**
 * The harness itself, because it is load-bearing.
 *
 * Every screen test in this console depends on `stubApi` answering each
 * endpoint with its own shape. When the console had ONE stub answering every
 * URL with `{"templates": []}`, `GET /auth/session` came back as a session with
 * no user and no orgs, and a suite of assertions ran against it and passed. A
 * test double that lies quietly is worse than no test.
 */
describe("stubApi", () => {
  it("answers each path with its own body", async () => {
    stubApi({
      "/auth/session": { body: sessionBody([{ slug: "acme", name: "Acme" }]) },
      "/templates": { body: { templates: [] } },
    })
    const session = await (await fetch("https://api/v1/auth/session")).json()
    const templates = await (await fetch("https://api/v1/templates")).json()
    expect(session).toMatchObject({ orgs: [{ slug: "acme" }] })
    expect(templates).toEqual({ templates: [] })
  })

  it("prefers the longest matching path, so a prefix cannot shadow a specific one", async () => {
    stubApi({
      "/auth/magic-link": { status: 202 },
      "/auth/magic-link/verify": { body: sessionBody([]) },
    })
    const verified = await fetch("https://api/v1/auth/magic-link/verify")
    expect(verified.status).toBe(200)
    expect((await fetch("https://api/v1/auth/magic-link")).status).toBe(202)
  })

  it("rejects an unstubbed path, naming it", async () => {
    stubApi({ "/auth/session": { body: sessionBody() } })
    await expect(fetch("https://api/v1/projects")).rejects.toThrow(/no stub for .*\/projects/)
  })

  it("records the method, the lower-cased headers and the parsed body", async () => {
    const { calls } = stubApi({ "/orgs": { status: 201, body: {} } })
    await fetch("https://api/v1/orgs", {
      method: "POST",
      headers: { "X-Halyard-Org": "acme", "content-type": "application/json" },
      body: JSON.stringify({ name: "Acme" }),
    })
    expect(calls).toEqual([
      {
        url: "https://api/v1/orgs",
        method: "POST",
        headers: { "x-halyard-org": "acme", "content-type": "application/json" },
        body: { name: "Acme" },
      },
    ])
  })

  it("returns no body at all for a 202 or a 204, because reading one throws", async () => {
    stubApi({ "/auth/magic-link": { status: 202 }, "/auth/session": { status: 204 } })
    const accepted = await fetch("https://api/v1/auth/magic-link")
    const noContent = await fetch("https://api/v1/auth/session")
    expect(accepted.status).toBe(202)
    expect(await accepted.text()).toBe("")
    expect(noContent.status).toBe(204)
  })

  it("builds the envelope `common.schema.json` defines, omitting what was not given", () => {
    expect(envelope({ code: "conflict", message: "Taken." })).toEqual({
      error: { code: "conflict", message: "Taken.", retriable: false },
    })
    expect(envelope({ code: "x", message: "y", fix: "z", request_id: "req_1" })).toEqual({
      error: { code: "x", message: "y", fix: "z", retriable: false, request_id: "req_1" },
    })
  })
})
