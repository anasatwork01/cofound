import { afterEach, describe, expect, it, vi } from "vitest"
import { ApiError, fetchTemplates, shouldRetry } from "@/lib/api"

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

  it("never apologises", async () => {
    for (const status of [401, 403, 404, 429, 500, 418]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => new Response("", { status })),
      )
      const error = (await fetchTemplates().catch((caught: unknown) => caught)) as ApiError
      expect(`${error.message} ${error.fix ?? ""}`.toLowerCase()).not.toMatch(
        /sorry|apolog|oops|unfortunately/,
      )
      expect(error.fix).toBeTruthy()
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
