/**
 * Task 0.13. The console ships client-side Sentry only, for reasons measured
 * and written out at the top of `instrumentation-client.ts`.
 *
 * That decision is expressed entirely by files that DO NOT EXIST, which is the
 * one kind of decision a reviewer cannot see in a diff and a well-meaning agent
 * will undo in thirty seconds by following a tutorial. These tests are what
 * turns "we chose not to" into something that fails.
 */
import { existsSync } from "node:fs"
import { fileURLToPath } from "node:url"

import { describe, expect, it } from "vitest"

const appRoot = new URL("../../", import.meta.url)

function inApp(relativePath: string): string {
  return fileURLToPath(new URL(relativePath, appRoot))
}

/**
 * The filenames Next.js treats as server- or edge-side instrumentation entries.
 *
 * Next resolves each of these from the app root and from `src/`, so both are
 * listed — putting `instrumentation.ts` in `src/` is the obvious way to think
 * you have worked around a rule about the root.
 */
const SERVER_SIDE_ENTRIES = [
  "instrumentation.ts",
  "instrumentation.js",
  "src/instrumentation.ts",
  "src/instrumentation.js",
  "sentry.server.config.ts",
  "sentry.server.config.js",
  "src/sentry.server.config.ts",
  "sentry.edge.config.ts",
  "sentry.edge.config.js",
  "src/sentry.edge.config.ts",
]

describe("the console's Sentry surface", () => {
  it.each(SERVER_SIDE_ENTRIES)("does not define %s", (entry) => {
    expect(
      existsSync(inApp(entry)),
      `apps/console/${entry} exists. The console ships client-side Sentry only: the server SDK ` +
        `was measured at +3.46 MiB and +0.09 s of Worker cold start, against a 1 s startup-CPU ` +
        `limit enforced at deploy (error 10021), and an unset DSN does not make it free. Read the ` +
        `comment at the top of instrumentation-client.ts, get the startup_time_ms measurement it ` +
        `asks for, and record it in docs/verified.md before adding this back.`,
    ).toBe(false)
  })

  it("defines instrumentation-client.ts at the app root, where Next looks for it", () => {
    // Not in `src/`: a root `instrumentation.ts` breaks the OpenNext build under
    // Turbopack, and the sibling filename is easy to "tidy" into src/ by
    // analogy. Next would then never load it and Sentry would be silently off.
    expect(existsSync(inApp("instrumentation-client.ts"))).toBe(true)
  })
})

describe("next.config.ts", () => {
  it("is wrapped by withSentryConfig", async () => {
    const config = (await import("../../next.config")).default as Record<string, unknown>
    const injected = Object.keys((config.env ?? {}) as object).filter((key) =>
      key.startsWith("_sentry"),
    )
    expect(injected.length, "withSentryConfig injects _sentry* build values").toBeGreaterThan(0)
  })

  it("keeps the decisions the wrapper is wrapping", async () => {
    // `withSentryConfig` returns a NEW object. Anything it does not know about
    // it copies, so a mistake here does not throw — it drops a setting. The two
    // that matter carry verified facts: `images.unoptimized` is task 0.12's
    // Cloudflare Images decision (a configured-but-unbound optimizer 500s at
    // runtime), and strict mode is how the console surfaces double-render bugs.
    const config = (await import("../../next.config")).default as {
      images?: { unoptimized?: boolean }
      reactStrictMode?: boolean
    }
    expect(config.images?.unoptimized).toBe(true)
    expect(config.reactStrictMode).toBe(true)
  })

  it("emits no browser source maps, so none are served from .next/static", async () => {
    // Sentry turns `productionBrowserSourceMaps` on when it intends to upload.
    // With uploads disabled it must leave it alone: maps in `.next/static` are
    // public, and publishing the console's sources is not a thing to do by
    // accident on the way to an error tracker.
    const config = (await import("../../next.config")).default as {
      productionBrowserSourceMaps?: boolean
    }
    expect(config.productionBrowserSourceMaps ?? false).toBe(false)
  })
})
