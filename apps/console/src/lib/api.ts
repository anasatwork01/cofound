/**
 * The console's HTTP client for `api`.
 *
 * Every shape here comes from `@halyard/schema/api-v1`, which is generated from
 * `packages/schema/api.openapi.yaml` (CLAUDE.md working agreement 1: never
 * hand-write the same type twice). Nothing in this file describes a payload of
 * its own.
 *
 * Only endpoints that exist in that document are reachable from here. In
 * particular there is NO credits call: `api.openapi.yaml`'s own header puts
 * credits and the ledger in phase 4, and CLAUDE.md working agreement 4 forbids
 * writing hopeful code against an endpoint nobody has specified. Task 4.9 adds
 * the call and the gauge that reads it.
 */
import type { components, operations } from "@halyard/schema/api-v1"

type Json200<Op extends { responses: { 200: { content: { "application/json": unknown } } } }> =
  Op["responses"][200]["content"]["application/json"]

/**
 * The request body of an operation whose body `api.openapi.yaml` writes inline
 * rather than naming a component for. `POST /auth/magic-link` is three lines of
 * YAML in the operation itself, so there is no `components["schemas"]` entry to
 * import and this reaches the same declaration through `operations`.
 */
type JsonBody<Op extends { requestBody: { content: { "application/json": unknown } } }> =
  Op["requestBody"]["content"]["application/json"]

export type AuthSession = components["schemas"]["AuthSession"]
export type AuthOrgMembership = components["schemas"]["AuthOrgMembership"]
export type TemplateListResponse = Json200<operations["listTemplates"]>
export type ProjectListResponse = Json200<operations["listProjects"]>
export type Template = components["schemas"]["Template"]
export type Project = components["schemas"]["Project"]
export type Org = components["schemas"]["Org"]
export type CreateOrgRequest = components["schemas"]["CreateOrgRequest"]
export type CreateProjectRequest = components["schemas"]["CreateProjectRequest"]
export type MagicLinkRequest = JsonBody<operations["requestMagicLink"]>
export type VerifyMagicLinkRequest = JsonBody<operations["verifyMagicLink"]>

/**
 * `servers[0]` in `api.openapi.yaml`. The env var exists so a preview
 * deployment and `next dev` can point somewhere else; the default is the
 * documented production server rather than a guess.
 */
export const API_BASE_URL = process.env.NEXT_PUBLIC_HALYARD_API_URL ?? "https://api.halyard.dev/v1"

/**
 * `api.openapi.yaml`'s `OrgContext` parameter: which organisation a request is
 * about, by slug.
 *
 * SPEC §6 makes a project slug unique only within an org, so `/projects` is
 * ambiguous for anyone who belongs to two orgs — and `POST /projects` names no
 * org in its path at all. Without this header the API answers "That request
 * needs an organisation." rather than guessing, which is the right refusal and
 * a bad screen, so every project call the console makes sends it.
 */
export const ORG_HEADER = "X-Halyard-Org"

/**
 * Where the Google button goes. A full-page navigation, not a fetch: the
 * endpoint answers 303 to Google and sets the CSRF and PKCE cookies on the way,
 * neither of which survives being read by `fetch`.
 */
export const GOOGLE_SIGN_IN_URL = `${API_BASE_URL}/auth/google/start`

/**
 * A failed request, carrying the error envelope `common.schema.json` defines:
 * a stable `code` to branch on, a `message` saying what happened and a `fix`
 * saying what to do about it. SPEC §18 requires both halves in the interface,
 * which is why `fix` is kept separate rather than concatenated into `message`.
 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fix: string | undefined
  readonly retriable: boolean
  readonly requestId: string | undefined

  constructor(init: {
    status: number
    code: string
    message: string
    fix?: string | undefined
    retriable: boolean
    requestId?: string | undefined
  }) {
    super(init.message)
    this.name = "ApiError"
    this.status = init.status
    this.code = init.code
    this.fix = init.fix
    this.retriable = init.retriable
    this.requestId = init.requestId
  }
}

/** The API's own answer to "am I signed in": 401, and nothing else. */
export function isUnauthenticated(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}

function isErrorEnvelope(body: unknown): body is components["schemas"]["ErrorResponse"] {
  if (typeof body !== "object" || body === null || !("error" in body)) return false
  const error: unknown = (body as { error: unknown }).error
  return (
    typeof error === "object" &&
    error !== null &&
    typeof (error as { code?: unknown }).code === "string" &&
    typeof (error as { message?: unknown }).message === "string"
  )
}

/**
 * A response the server could not describe — a proxy's 502 page, a 404 from a
 * route that does not exist yet. The copy still has to obey SPEC §18: say what
 * happened, say what to do, do not apologise.
 */
function undescribedFailure(status: number): { message: string; fix: string } {
  if (status === 401) {
    return { message: "Your session has expired.", fix: "Sign in again to carry on." }
  }
  if (status === 403) {
    return {
      message: "Your role does not allow this.",
      fix: "Ask an owner or admin of this organisation to do it, or to change your role.",
    }
  }
  if (status === 404) {
    return { message: "That is not here.", fix: "Check the address, or go back and try again." }
  }
  if (status === 429) {
    return { message: "Too many requests just now.", fix: "Wait a moment and try again." }
  }
  if (status >= 500) {
    return { message: "The server could not answer.", fix: "Try again in a moment." }
  }
  return { message: "The request was refused.", fix: "Try again, or reload the page." }
}

async function toApiError(response: Response): Promise<ApiError> {
  let body: unknown = null
  try {
    body = await response.json()
  } catch {
    body = null
  }
  if (isErrorEnvelope(body)) {
    const { error } = body
    return new ApiError({
      status: response.status,
      code: error.code,
      message: error.message,
      fix: error.fix,
      retriable: error.retriable,
      requestId: error.request_id,
    })
  }
  const fallback = undescribedFailure(response.status)
  return new ApiError({
    status: response.status,
    code: `http_${response.status}`,
    message: fallback.message,
    fix: fallback.fix,
    retriable: response.status === 429 || response.status >= 500,
  })
}

type Send = {
  readonly method: "GET" | "POST" | "DELETE"
  /** Sent as JSON. `undefined` means no body at all, not `null`. */
  readonly body?: unknown
  /** Org slug for `X-Halyard-Org`. */
  readonly org?: string | undefined
}

/**
 * One place where a request is made, so the cookie, the headers and the error
 * envelope cannot be got right on some calls and wrong on others.
 *
 * NO `Idempotency-Key`, deliberately, and not by oversight. SPEC §7.1's header
 * exists so a client that never learned whether its request landed can retry
 * it; this console does not retry mutations (see `providers.tsx`), so a
 * freshly generated key per attempt would buy nothing. A key held across
 * attempts is worse than nothing: the API fingerprints the body, so a user who
 * corrects a field the server rejected and submits again is answered "That
 * Idempotency-Key was already used for a different request." Double submits
 * are handled where they happen — a form ignores a submit while its mutation
 * is in flight. When something here genuinely retries, it sends a key minted
 * for that body.
 */
async function send(path: string, init: Send): Promise<Response> {
  const headers: Record<string, string> = { accept: "application/json" }
  if (init.body !== undefined) headers["content-type"] = "application/json"
  if (init.org !== undefined) headers[ORG_HEADER] = init.org

  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: init.method,
    // SPEC §8 puts the console's session in an httpOnly cookie, and the API is
    // on another origin, so it has to be sent explicitly.
    credentials: "include",
    headers,
    ...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
  })
  if (!response.ok) throw await toApiError(response)
  return response
}

async function getJson<T>(path: string, org?: string): Promise<T> {
  const response = await send(path, { method: "GET", org })
  return (await response.json()) as T
}

/**
 * TanStack Query's retry predicate.
 *
 * A 4xx is the server saying "not like that", and repeating it wastes the
 * user's time; the envelope's own `retriable` flag decides. Anything that never
 * reached a server — DNS, offline, a dropped connection — is worth one retry.
 */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= 2) return false
  if (error instanceof ApiError) return error.retriable
  return true
}

/**
 * Query keys, in one place so an invalidation cannot miss a cache by spelling
 * its key differently.
 *
 * `projects` takes the org because the list IS org-scoped — the same user in
 * two orgs has two different lists behind the same path, separated only by
 * `X-Halyard-Org`. A single flat key would serve one org's projects to the
 * other on a switch, which is a tenancy bug that looks like a caching one.
 */
export const queryKeys = {
  session: ["session"] as const,
  templates: ["templates"] as const,
  projects: (org: string) => ["projects", org] as const,
}

/** SPEC §7.1 `GET /auth/session` — who am I, and which orgs am I in. */
export function fetchSession(): Promise<AuthSession> {
  return getJson<AuthSession>("/auth/session")
}

/**
 * SPEC §7.1 `GET /templates` — the gallery behind `/new` (SPEC §18).
 *
 * A 404 is read as "none installed", not as a failure, and that is a statement
 * about WHERE the templates come from rather than a swallowed error. The
 * registry is task 2.1: until it ships, the API mounts no `/v1/templates` route
 * at all, so the collection answers 404 for the same reason it would answer an
 * empty list — there are no templates. The gallery's empty state already says
 * the true and useful thing ("No templates are installed yet. Describe what you
 * want in the box above instead"), whereas the error state offers a "Try again"
 * that cannot ever succeed, on the console's primary screen.
 *
 * DELETE THIS when task 2.1 mounts the route. After that a 404 here means a
 * broken deployment and should be loud. Nothing else is caught: a 401, a 403, a
 * 500 and a dropped connection all still reach `ErrorNotice` unchanged.
 */
export async function fetchTemplates(): Promise<TemplateListResponse> {
  try {
    return await getJson<TemplateListResponse>("/templates")
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return { templates: [] }
    }
    throw error
  }
}

/** SPEC §7.1 `GET /projects` — the project list, for one organisation. */
export function fetchProjects(org: string): Promise<ProjectListResponse> {
  return getJson<ProjectListResponse>("/projects", org)
}

/**
 * `POST /auth/magic-link` — email a sign-in link.
 *
 * Resolves on 202 and carries nothing back, because the endpoint answers 202
 * whether or not the address has an account. That is an anti-enumeration
 * property, not an omission: a response that differed for a known address would
 * turn this into a way to test whether a given person has one. Anything the
 * console showed afterwards that implied an account was found would give away
 * exactly what the 202 is protecting.
 */
export async function requestMagicLink(body: MagicLinkRequest): Promise<void> {
  const response = await send("/auth/magic-link", { method: "POST", body })
  // Drain the 202's empty body, which nothing else here needs.
  //
  // Not a formality. A `fetch` whose Response body is never consumed has that
  // body stream CANCELLED when it is collected, and Chromium records the whole
  // request as `net::ERR_ABORTED` — measured, and it is why this line exists.
  // The request succeeded, the API answered 202 and the link was sent; only the
  // browser's own accounting disagrees. It shows up in DevTools' network panel
  // as a failed POST to `/auth/magic-link`, on the sign-in screen, which is
  // precisely where someone looks when sign-in is not working.
  await response.text()
}

/**
 * `POST /auth/magic-link/verify` — exchange the token in an emailed link for a
 * session. Sets the session cookie; the returned body is who you now are.
 */
export async function verifyMagicLink(body: VerifyMagicLinkRequest): Promise<AuthSession> {
  const response = await send("/auth/magic-link/verify", { method: "POST", body })
  return (await response.json()) as AuthSession
}

/**
 * `POST /orgs` — create an organisation.
 *
 * The caller becomes its owner in the same transaction, so the session's `orgs`
 * list is stale the moment this resolves and whoever calls it invalidates
 * `queryKeys.session`.
 */
export async function createOrg(body: CreateOrgRequest): Promise<Org> {
  const response = await send("/orgs", { method: "POST", body })
  return (await response.json()) as Org
}

/**
 * `POST /projects` — create a project.
 *
 * Needs the org twice over, and both are load-bearing: `X-Halyard-Org` selects
 * the tenancy scope the request runs in, and `org_id` in the body says which
 * org the caller believes they are creating in. The API refuses when they
 * disagree rather than honouring the body, so a request cannot create a project
 * in an org whose role was never checked.
 */
export async function createProject(body: CreateProjectRequest, org: string): Promise<Project> {
  const response = await send("/projects", { method: "POST", body, org })
  return (await response.json()) as Project
}

/** `DELETE /auth/session` — sign out. Idempotent, and answers 204 with no body. */
export async function signOut(): Promise<void> {
  await send("/auth/session", { method: "DELETE" })
}
