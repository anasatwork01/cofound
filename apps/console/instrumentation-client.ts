/**
 * Sentry for the console — CLIENT SIDE ONLY. Task 0.13, SPEC §20.
 *
 * There is no `instrumentation.ts`, no `sentry.server.config.ts` and no
 * `sentry.edge.config.ts` in this app, and their absence is the design. Please
 * read this before "finishing" the setup by adding them.
 *
 * WHY THE SERVER HALF IS NOT HERE
 *
 * The console is a Worker, deployed through `@opennextjs/cloudflare`. Adding
 * the Sentry server SDK was measured, not guessed:
 *
 *   - the Worker's server bundle went from 3,011,252 to 6,643,821 bytes
 *     (+3.46 MiB, +121%);
 *   - local cold start went from 0.131 s to 0.222 s.
 *
 * Bundle size is not what bites on Workers. Startup CPU is: a Worker gets one
 * second of it and a deploy that exceeds the limit is rejected outright with
 * error 10021. The measured change more than doubles the parse-and-execute cost
 * of every cold start, against a ceiling we have no headroom figure for.
 *
 * An unset DSN does not buy it back. Measured, with `SENTRY_DSN` removed: the
 * SDK still loads, still reports `isInitialized: true`, and still costs the same
 * bytes and the same startup CPU. So "ship it now, switch it on later" is not
 * available — the cost is paid at import, not at `init`.
 *
 * Client-only, by contrast, is nearly free where it matters. Measured on this
 * change, `handler.mjs` went 3,011,717 → 3,016,547 bytes: +4,830, +0.16%, and
 * none of it SDK. (The baseline differs from the 3,011,252 above by 465 bytes
 * because that measurement was taken a few commits earlier; the conclusion is
 * three orders of magnitude away from caring.) The added bytes are the
 * serialised Next config the wrapper adds
 * (`serverExternalPackages`, `clientTraceMetadata`, `_sentry*` env values).
 * There is no Sentry runtime in the Worker at all. The SDK's ~148 KiB lands in
 * `.next/static`, which Cloudflare serves as an asset and which is never parsed
 * during Worker startup. If that handler number ever jumps by megabytes,
 * something server-side has leaked in and this comment is the thing you are
 * looking for.
 *
 * WHAT WOULD UNBLOCK IT
 *
 * One number: `startup_time_ms` from a real `wrangler deploy` of the console
 * with the server SDK included, compared against the 1 s limit. That needs a
 * Cloudflare account and a Sentry account, and it is blocked on SPEC §21
 * decision 1. Until somebody has that measurement, adding the server SDK would
 * mean depending on an unverified platform fact, which CLAUDE.md working
 * agreement 2 forbids. Get the number, record it in `docs/verified.md`, then
 * revisit — do not add the files because a tutorial lists four of them.
 *
 * Nothing is unobserved in the meantime. The Worker's own request logs are on
 * via `observability: { enabled: true }` in `wrangler.jsonc`, the Go services
 * get the real Sentry SDK, and for a dashboard the errors users actually hit —
 * hydration failures, fetch failures, render crashes — happen in the browser,
 * which is exactly what this file covers.
 *
 * ALSO: do not add a root `instrumentation.ts` as a way in. Under Next 16's
 * Turbopack build it breaks the OpenNext build outright. This file is a
 * different thing: `instrumentation-client.ts` is a client entry, and the app
 * root is where Next.js expects to find it.
 *
 * WHAT LEAVES THIS BROWSER (SPEC §17)
 *
 * The console holds `__Host-halyard_session`. `clientSentryOptions()` pins
 * every field of Sentry's `dataCollection` off — no cookies, no request or
 * response headers, no bodies, no query parameters, no user identity — so the
 * session cookie has no path to a third party by configuration rather than by
 * the vendor's default. See the comments there; the settings are exhaustive on
 * purpose and typed so that omitting one fails the build.
 *
 * No source maps are uploaded either, so no source leaves the build. See
 * `next.config.ts`.
 */
import * as Sentry from "@sentry/nextjs"

import { clientSentryOptions } from "./src/lib/sentry-options"

/**
 * Each variable is spelled out as a literal `process.env.X` member access.
 *
 * That is a requirement, not a style: Next.js inlines client-side environment
 * variables by substituting that exact expression at build time, and nothing
 * else. Checked by building it both ways and reading the chunk:
 *
 *   - spelled out, the chunk contains
 *     `NEXT_PUBLIC_SENTRY_DSN:"https://…@o999.ingest.sentry.io/4321"`;
 *   - written as `clientSentryOptions(process.env)`, the DSN does not appear in
 *     `.next/static` anywhere.
 *
 * So the tidier-looking version disables Sentry in production while passing
 * every test in this repository. Destructuring fails the same way. (The same
 * build shows `HALYARD_ENV` surviving only as a runtime lookup on Next's
 * `process` shim — that is expected; see `resolveSentryEnvironment`.)
 */
function init(): void {
  const options = clientSentryOptions({
    NEXT_PUBLIC_SENTRY_DSN: process.env.NEXT_PUBLIC_SENTRY_DSN,
    NEXT_PUBLIC_HALYARD_ENV: process.env.NEXT_PUBLIC_HALYARD_ENV,
    HALYARD_ENV: process.env.HALYARD_ENV,
  })

  // No DSN, no SDK. Returning here — rather than calling `init` with an empty
  // `dsn`, or with `enabled: false` — is what keeps a developer with no Sentry
  // account from getting global error handlers, a patched `fetch` and a patched
  // `history` for nothing. The import cost remains, and is paid by the browser
  // only; it never touches the Worker's startup CPU budget.
  if (!options) {
    return
  }

  Sentry.init(options)
}

init()

/**
 * App Router navigations. Next.js calls this hook on a client-side route
 * change, and without it a navigation is invisible to Sentry — errors land
 * attributed to whichever page happened to load first.
 *
 * Safe to export unconditionally: with no client initialised it is a no-op.
 */
export const onRouterTransitionStart = Sentry.captureRouterTransitionStart
