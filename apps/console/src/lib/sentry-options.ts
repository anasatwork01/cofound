/**
 * The browser Sentry options, built as a value so they can be tested.
 *
 * `instrumentation-client.ts` is a Next.js client entry: it is evaluated once,
 * in a browser, before the app hydrates. Nothing inside it is reachable from a
 * unit test. So the decisions live here — a pure function of the environment —
 * and the entry is reduced to "build the options, init if there are any".
 *
 * Task 0.13. SPEC §17 (trust boundary), SPEC §20 (observability).
 */
import type { BrowserOptions } from "@sentry/nextjs"

/**
 * The environment variables this module reads, passed in rather than taken off
 * `process.env` directly.
 *
 * This is not only for testability. Next.js inlines `process.env.NEXT_PUBLIC_*`
 * into the client bundle by *static member access substitution* — it rewrites
 * the exact text `process.env.NEXT_PUBLIC_SENTRY_DSN`, it does not ship a
 * populated `process.env` object. Handing this function `process.env` itself
 * therefore leaves the DSN undefined in the browser, which is Sentry silently
 * off in production with every test still green. The caller must spell each
 * name out; this signature is what forces it to. See the comment on `init()` in
 * `instrumentation-client.ts` for the build output either way.
 */
export type ConsoleEnv = Readonly<Record<string, string | undefined>>

/** Used when neither environment variable is set, e.g. `next dev` locally. */
export const DEFAULT_ENVIRONMENT = "development"

/**
 * `NEXT_PUBLIC_HALYARD_ENV` first, `HALYARD_ENV` second.
 *
 * `HALYARD_ENV` is the name `wrangler.jsonc` binds per environment, but that is
 * a Worker `var`: it exists on the server at runtime and does NOT reach the
 * browser. Confirmed in the built chunk — `NEXT_PUBLIC_HALYARD_ENV` is
 * substituted for its literal value while `HALYARD_ENV` compiles to
 * `E.default.env.HALYARD_ENV`, a lookup on Next's `process` shim that resolves
 * to undefined there. It is read anyway as a second chance for a build that
 * exports it as a plain build-time variable, and so this function is honest
 * when called from somewhere other than a browser. `NEXT_PUBLIC_HALYARD_ENV` is
 * the one that actually works, which is why it wins.
 *
 * If console events ever arrive tagged "development" from a real deployment,
 * this is why: the deploy set `HALYARD_ENV` and not `NEXT_PUBLIC_HALYARD_ENV`,
 * and the latter has to be present at BUILD time, not at deploy time.
 */
export function resolveSentryEnvironment(env: ConsoleEnv): string {
  const value = env.NEXT_PUBLIC_HALYARD_ENV?.trim() || env.HALYARD_ENV?.trim()
  return value || DEFAULT_ENVIRONMENT
}

/**
 * Full sampling everywhere except production, where 10% of navigations is
 * enough to see a regression without paying for a span on every page view.
 * Errors are never sampled down — `sampleRate` stays 1.
 */
export function tracesSampleRateFor(environment: string): number {
  return environment === "production" ? 0.1 : 1
}

/**
 * Every field of Sentry's `dataCollection`, with none optional.
 *
 * This type is a guard, and it is the reason the block below spells out
 * settings that look like they are already the default.
 *
 * `resolveDataCollectionOptions()` in `@sentry/core` chooses its baseline like
 * this: if `dataCollection` is present *at all*, every field you omit falls
 * back to `DEFAULTS`, which is the permissive set — `cookies: true`,
 * `httpHeaders: { request: true, response: true }`, all four `httpBodies`. It
 * is only when `dataCollection` is absent entirely that the conservative
 * `sendDefaultPii: false` baseline applies. So a partial `dataCollection`
 * intended to tighten one field silently loosens the rest.
 *
 * Requiring every field means `tsc` fails rather than the console quietly
 * starting to ship cookies, and it fails again the day Sentry adds a field we
 * have not considered.
 */
type DataCollection = NonNullable<BrowserOptions["dataCollection"]>
type ExhaustiveDataCollection = Required<Omit<DataCollection, "queryParams">> & {
  httpHeaders: Required<NonNullable<DataCollection["httpHeaders"]>>
  graphQL: Required<NonNullable<DataCollection["graphQL"]>>
  genAI: Required<NonNullable<DataCollection["genAI"]>>
}

/**
 * What the console is allowed to send to Sentry: a stack trace, a URL, and the
 * breadcrumbs leading to it. No cookies, no headers, no bodies, no identity.
 *
 * SPEC §17 is the reason. The console holds `__Host-halyard_session`, the
 * cookie that *is* the user's session (services/api/internal/auth/cookie.go) —
 * anything that could carry it to a third party is a session-forwarding bug
 * with a vendor's name on it. The SDK's own filter list happens to redact
 * `__host-` prefixed cookies today, but a vendor's denylist is not a trust
 * boundary: it is one dependency bump away from changing. Not collecting is.
 *
 * `urlQueryParams: false` for the same reason one level down — OAuth callbacks
 * and signed links put credentials in query strings, and the console's own
 * routes carry tenant identifiers.
 *
 * `genAI` is off because the console renders the user's prompts and the agent's
 * output. Those are the customer's product ideas and the customer's code.
 */
const DATA_COLLECTION: ExhaustiveDataCollection = {
  userInfo: false,
  cookies: false,
  httpHeaders: { request: false, response: false },
  httpBodies: [],
  urlQueryParams: false,
  graphQL: { document: false, variables: false },
  genAI: { inputs: false, outputs: false },
  databaseQueryData: false,
  // Browser-side this is inert — local-variable capture is a Node integration —
  // but a local can hold a token, so it is off explicitly rather than by luck.
  stackFrameVariables: false,
  // Source context around a frame. Not user data; the documented default.
  frameContextLines: 5,
}

/**
 * The options to hand `Sentry.init`, or `undefined` when there is no DSN.
 *
 * `undefined` is the whole point of the return type. Task 0.13 lands before a
 * Sentry account exists (SPEC §21 decision 1), so the overwhelmingly common
 * case is an unset DSN, and in that case the caller must not call `init` at
 * all. An initialised client with no DSN is not inert: it still installs global
 * error and `unhandledrejection` handlers, still patches `fetch` and `history`
 * for breadcrumbs, and still reports `isInitialized: true`. A developer with no
 * Sentry account should get none of that.
 */
export function clientSentryOptions(env: ConsoleEnv): BrowserOptions | undefined {
  const dsn = env.NEXT_PUBLIC_SENTRY_DSN?.trim()
  if (!dsn) {
    return undefined
  }

  const environment = resolveSentryEnvironment(env)

  // No `replayIntegration`, deliberately: Session Replay records the DOM, and
  // the console's DOM contains the user's prompts, their code and their
  // customers' data.
  //
  // No `tracePropagationTargets` either, which is a decision and not an
  // omission. Left unset, the browser SDK attaches `sentry-trace` and `baggage`
  // to same-origin requests only (`shouldAttachHeaders()` in @sentry/browser).
  // `api` is a different origin — NEXT_PUBLIC_HALYARD_API_URL defaults to
  // https://api.halyard.dev/v1 — so no trace header crosses to it, and none
  // crosses to anyone else either. Joining the console span to the `api` span
  // is SPEC §20's OpenTelemetry job and would need a CORS allowance on the Go
  // gateway first; naming an origin here would only send Sentry's headers to a
  // service that rejects the preflight.
  return {
    dsn,
    environment,
    // Every error; a sample of the traces.
    sampleRate: 1,
    tracesSampleRate: tracesSampleRateFor(environment),
    dataCollection: DATA_COLLECTION,
  }
}
