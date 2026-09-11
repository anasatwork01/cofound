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

export default nextConfig
