/**
 * Test doubles with no JSX in them, and that is the whole reason this file is
 * separate from `test-utils.tsx`.
 *
 * `vitest.config.mts` runs two projects: `dom` (happy-dom, with the React
 * plugin) takes `*.test.tsx`, and `node` takes `*.test.ts`. The node project
 * has no JSX transform, so a `.test.ts` that imports a `.tsx` module fails to
 * parse before a single assertion runs. `lib/api.test.ts` needs these; it must
 * not need a renderer.
 */
import { vi } from "vitest"

export type StubbedResponse = {
  readonly status?: number
  readonly body?: unknown
}

export type StubbedCall = {
  readonly url: string
  readonly method: string
  readonly headers: Record<string, string>
  readonly body: unknown
}

/**
 * `fetch`, stubbed per API path.
 *
 * Every screen in this console reads more than one endpoint — `/new` alone asks
 * for the session, the project list and the templates — so a single stub that
 * answers every URL with the same body is not a test double, it is a way of
 * making three different requests look like one. The console used to have
 * exactly that, and it answered `GET /auth/session` with `{"templates": []}`:
 * a session object with no user and no orgs in it, which every assertion then
 * ran against.
 *
 * So this routes by path, and a path nobody stubbed REJECTS, with the URL in
 * the message, rather than being answered with a default. It is a diagnostic,
 * not a guard: the component under test still gets to handle that rejection,
 * and a test asserting only on copy can still pass. What it removes is the
 * silent wrong answer — a body shaped like some other endpoint's.
 */
export function stubApi(routes: Readonly<Record<string, StubbedResponse>>): {
  readonly calls: readonly StubbedCall[]
  readonly callsTo: (path: string) => readonly StubbedCall[]
} {
  // Longest first, so `/projects` cannot shadow a more specific `/projects/x`.
  const paths = Object.keys(routes).sort((a, b) => b.length - a.length)
  const calls: StubbedCall[] = []

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: unknown, init?: RequestInit) => {
      const url = String(input)
      const headers: Record<string, string> = {}
      for (const [name, value] of Object.entries((init?.headers ?? {}) as Record<string, string>)) {
        headers[name.toLowerCase()] = value
      }
      calls.push({
        url,
        method: init?.method ?? "GET",
        headers,
        body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
      })

      const match = paths.find((path) => url.includes(path))
      if (match === undefined) {
        throw new Error(
          `stubApi: no stub for ${url}. Add the path to the routes map — a default answer ` +
            `would make this test pass against an endpoint it never described.`,
        )
      }
      const { status = 200, body } = routes[match] ?? {}
      // 202 and 204 carry no body, and `response.json()` on one throws.
      if (status === 202 || status === 204) return new Response(null, { status })
      return new Response(JSON.stringify(body ?? {}), {
        status,
        headers: { "content-type": "application/json" },
      })
    }),
  )

  return {
    calls,
    callsTo: (path: string) => calls.filter((call) => call.url.includes(path)),
  }
}

/** The error envelope `common.schema.json` defines, for a test that needs one. */
export function envelope(init: {
  code: string
  message: string
  fix?: string
  retriable?: boolean
  request_id?: string
}): unknown {
  return {
    error: {
      code: init.code,
      message: init.message,
      ...(init.fix === undefined ? {} : { fix: init.fix }),
      retriable: init.retriable ?? false,
      ...(init.request_id === undefined ? {} : { request_id: init.request_id }),
    },
  }
}

/** A signed-in `GET /auth/session` body, with as many orgs as the test wants. */
export function sessionBody(
  orgs: ReadonlyArray<{ id?: string; slug: string; name: string; role?: string }> = [],
): unknown {
  return {
    user: { id: "11111111-1111-4111-8111-111111111111", email: "founder@example.com", name: null },
    orgs: orgs.map((org, index) => ({
      id: org.id ?? `2222222${index}-2222-4222-8222-222222222222`,
      slug: org.slug,
      name: org.name,
      role: org.role ?? "owner",
    })),
  }
}
