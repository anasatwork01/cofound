import { withSentryConfig } from "@sentry/nextjs/config"
import type { NextConfig } from "next"

/**
 * The console is an authenticated dashboard: dynamic SSR plus TanStack Query,
 * with no ISR surface. That is why `open-next.config.ts` needs no cache
 * bindings at all — see the comment there.
 *
 * Deliberately absent:
 *
 *   - `runtime: "edge"` on any route. The OpenNext adapter targets Next's
 *     Node.js runtime specifically, because the edge runtime does not support
 *     every Next feature. Declaring edge would opt us out of the runtime the
 *     adapter is built for (docs/verified.md, SPEC §22 item 1).
 *   - Node.js middleware. Supported by the adapter only as an explicitly
 *     experimental, unmaintained path. Use edge middleware if auth redirects
 *     ever need one.
 *   - `initOpenNextCloudflareForDev()`. It exists to expose Cloudflare bindings
 *     to `next dev`, and the console has no bindings. Add it with the first one.
 */
const nextConfig: NextConfig = {
  reactStrictMode: true,
  // No Cloudflare Images binding, so Next's optimizer has no backend here.
  // Task 0.12 decides whether to pay for Cloudflare Images; until then an
  // unoptimized <Image> is honest, where a configured-but-unbound optimizer
  // would 500 at runtime.
  images: { unoptimized: true },
}

/**
 * Sentry's build-time wrapper. Task 0.13.
 *
 * Imported from `@sentry/nextjs/config`, not `@sentry/nextjs`. The latter still
 * re-exports it, but marked `@deprecated` and removed in v11, and the build
 * prints a deprecation notice for it.
 *
 * This wrapper is build-time only. The runtime half is `instrumentation-client.ts`,
 * and that file is the whole of it — the console ships client-side Sentry only,
 * for measured reasons written out at the top of it. Do not read this wrapper as
 * evidence that a server config is missing.
 *
 * NO SOURCE MAPS ARE UPLOADED, for two separate reasons:
 *
 *   1. Server maps could not work anyway. `@opennextjs/cloudflare` 1.20.6 emits
 *      no source map from its own server bundling pass, so the chain from
 *      Next's output to the deployed Worker is already broken before Sentry is
 *      involved. Uploading Next's server maps would produce stack frames
 *      pointing into a file the Worker does not run.
 *   2. Client maps do work, but uploading them needs `SENTRY_AUTH_TOKEN` plus
 *      an org and project slug, and none of those exist yet — 0.13 lands before
 *      the Sentry account does (SPEC §21 decision 1). `disable: true` is what
 *      keeps the build from emitting maps, injecting debug IDs and attempting
 *      an upload it has no credentials for. Verified in the build output: no
 *      `.map` survives in `.next/static`.
 *
 * When the account exists, the client half is the part worth turning on: set
 * `org`, `project` and `SENTRY_AUTH_TOKEN`, and flip `sourcemaps.disable`. Set
 * `sourcemaps.deleteSourcemapsAfterUpload: true` in the same change, and do not
 * trust the option's own documentation about it — that says the default is
 * `true`, but the Turbopack path the console actually builds through reads it
 * as `sentryBuildOptions.sourcemaps?.deleteSourcemapsAfterUpload ?? false`
 * (`handleRunAfterProductionCompile.js` in @sentry/nextjs 10.74.0). Left alone,
 * the maps would be uploaded to Sentry AND left in `.next/static`, which is
 * served to anyone who asks for it.
 */
export default withSentryConfig(nextConfig, {
  sourcemaps: { disable: true },

  // Release creation is a Sentry API call, separate from source-map upload. It
  // is off because a `SENTRY_AUTH_TOKEN` that happens to be in the environment
  // — a shared org token in CI, say — would otherwise be enough for a build to
  // start writing releases into a Sentry organisation nobody configured here.
  //
  // It does NOT silence the "No auth token provided. Will not create release."
  // warning every build prints: `createRelease()` in @sentry/bundler-plugins
  // checks `authToken` and returns before it ever looks at `release.create`.
  // The cure for the warning is the auth token, not `silent: true` — silencing
  // the plugin wholesale would also hide a failed source-map upload later, and
  // this warning is telling the truth about a vendor we have not signed up for.
  //
  // The release NAME is still injected into the client bundle (the git SHA), so
  // events arrive tagged with the build that produced them from day one.
  release: { create: false },

  // Do not report this build to Sentry's own telemetry. There is no account to
  // attach it to, and a build step that phones a vendor by default is not
  // something to leave on by accident.
  telemetry: false,

  // Never on. A tunnel route would proxy every Sentry envelope through the
  // console's own Worker — third-party traffic on the origin that serves
  // `__Host-halyard_session`, billed to us, and an ad-blocker workaround the
  // user did not consent to. SPEC §17.
  tunnelRoute: false,

  // Next.js internals and dependencies stay out of any future upload. Sentry's
  // default, restated because flipping it is a common "fix" for unreadable
  // frames and it costs build time for code we do not own.
  widenClientFileUpload: false,

  // Client bundle only — these are define-flags the bundler tree-shakes on, not
  // server behaviour. Session Replay is never enabled (see
  // `src/lib/sentry-options.ts`), so all of its recording code can go.
  // `excludeTracing` is deliberately absent: tracing IS used.
  bundleSizeOptimizations: {
    excludeDebugStatements: true,
    excludeReplayShadowDom: true,
    excludeReplayIframe: true,
    excludeReplayWorker: true,
  },
})
