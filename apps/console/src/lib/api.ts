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

export type AuthSession = components["schemas"]["AuthSession"]
export type TemplateListResponse = Json200<operations["listTemplates"]>
export type ProjectListResponse = Json200<operations["listProjects"]>
export type Template = components["schemas"]["Template"]
export type Project = components["schemas"]["Project"]

/**
 * `servers[0]` in `api.openapi.yaml`. The env var exists so a preview
 * deployment and `next dev` can point somewhere else; the default is the
 * documented production server rather than a guess.
 */
export const API_BASE_URL = process.env.NEXT_PUBLIC_HALYARD_API_URL ?? "https://api.halyard.dev/v1"

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

async function getJson<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    method: "GET",
    // SPEC §8 puts the console's session in an httpOnly cookie, and the API is
    // on another origin, so it has to be sent explicitly.
    credentials: "include",
    headers: { accept: "application/json", ...init?.headers },
  })
  if (!response.ok) throw await toApiError(response)
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
 */
export const queryKeys = {
  session: ["session"] as const,
  templates: ["templates"] as const,
  projects: ["projects"] as const,
}

/** SPEC §7.1 `GET /auth/session` — who am I, and which orgs am I in. */
export function fetchSession(): Promise<AuthSession> {
  return getJson<AuthSession>("/auth/session")
}

/** SPEC §7.1 `GET /templates` — the gallery behind `/new` (SPEC §18). */
export function fetchTemplates(): Promise<TemplateListResponse> {
  return getJson<TemplateListResponse>("/templates")
}

/** SPEC §7.1 `GET /projects` — the project picker, and one day "last project". */
export function fetchProjects(): Promise<ProjectListResponse> {
  return getJson<ProjectListResponse>("/projects")
}
