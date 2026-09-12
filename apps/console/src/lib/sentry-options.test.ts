/**
 * Task 0.13. The two things about the console's Sentry setup that can be wrong
 * silently: it can be on when it should be off, and it can send more than it
 * should. Both are checked here against the real option builder rather than
 * against a restatement of it.
 */
import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { dirname, join } from "node:path"

import { describe, expect, it } from "vitest"

import {
  DEFAULT_ENVIRONMENT,
  clientSentryOptions,
  resolveSentryEnvironment,
  tracesSampleRateFor,
} from "./sentry-options"

const DSN = "https://examplePublicKey@o0.ingest.sentry.io/0"

/**
 * Every `dataCollection` category the installed @sentry/core knows about, read
 * out of the SDK's own `DEFAULTS` table.
 *
 * That table is where the permissive baseline lives, so it is the authoritative
 * list of things that get collected when you do not say otherwise. There is no
 * public export for it — `resolveDataCollectionOptions` is internal — so this
 * reads the built module. A version bump that moves or reshapes it fails loudly
 * here rather than quietly widening what the console sends.
 */
function sdkDataCollectionCategories(): string[] {
  const require = createRequire(import.meta.url)
  // @sentry/core is a transitive dependency, so resolve it through the package
  // that owns it rather than from here — under pnpm it is not a sibling.
  const corePackageJson = createRequire(require.resolve("@sentry/nextjs/package.json")).resolve(
    "@sentry/core/package.json",
  )
  const modulePath = join(
    dirname(corePackageJson),
    "build/esm/utils/data-collection/resolveDataCollectionOptions.js",
  )

  const defaults = /const DEFAULTS = \{\n([\s\S]*?)\n\};/.exec(readFileSync(modulePath, "utf8"))
  if (!defaults?.[1]) {
    throw new Error(
      `no DEFAULTS table in ${modulePath}. The SDK moved it; re-read how dataCollection ` +
        `resolves before trusting apps/console/src/lib/sentry-options.ts.`,
    )
  }

  const categories = [...defaults[1].matchAll(/^ {2}([A-Za-z_]\w*):/gm)].map((match) => match[1]!)
  expect(categories.length, `DEFAULTS parsed to ${categories.length} categories`).toBeGreaterThan(5)
  return categories.sort()
}

describe("clientSentryOptions", () => {
  it("returns nothing when there is no DSN, so init is never called", () => {
    expect(clientSentryOptions({})).toBeUndefined()
  })

  it("treats an empty or whitespace DSN as absent", () => {
    // `wrangler.jsonc` and `.env` files both make it easy to define a variable
    // as the empty string. An empty `dsn` is not an off switch in the SDK — it
    // throws or silently installs the handlers depending on version — so it has
    // to be caught here.
    expect(clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: "" })).toBeUndefined()
    expect(clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: "   " })).toBeUndefined()
  })

  it("initialises with the DSN when one is set", () => {
    const options = clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: ` ${DSN} ` })
    expect(options?.dsn).toBe(DSN)
    expect(options?.sampleRate).toBe(1)
  })
})

describe("resolveSentryEnvironment", () => {
  it("prefers the variable that actually reaches the browser", () => {
    expect(
      resolveSentryEnvironment({
        NEXT_PUBLIC_HALYARD_ENV: "production",
        HALYARD_ENV: "staging",
      }),
    ).toBe("production")
  })

  it("falls back to HALYARD_ENV, then to development", () => {
    expect(resolveSentryEnvironment({ HALYARD_ENV: "staging" })).toBe("staging")
    expect(resolveSentryEnvironment({})).toBe(DEFAULT_ENVIRONMENT)
    expect(resolveSentryEnvironment({ NEXT_PUBLIC_HALYARD_ENV: "" })).toBe(DEFAULT_ENVIRONMENT)
  })

  it("tags events with the environment it resolved", () => {
    const options = clientSentryOptions({
      NEXT_PUBLIC_SENTRY_DSN: DSN,
      NEXT_PUBLIC_HALYARD_ENV: "staging",
    })
    expect(options?.environment).toBe("staging")
  })
})

describe("tracesSampleRateFor", () => {
  it("samples production down and leaves everything else whole", () => {
    expect(tracesSampleRateFor("production")).toBe(0.1)
    expect(tracesSampleRateFor("staging")).toBe(1)
    expect(tracesSampleRateFor(DEFAULT_ENVIRONMENT)).toBe(1)
  })
})

/**
 * SPEC §17. The console holds `__Host-halyard_session`; nothing that could
 * carry it may be collected by default.
 *
 * This walks the whole `dataCollection` object instead of naming fields, so it
 * fails on any field turned on — including one added later that this file has
 * never heard of. `frameContextLines` is the single allowed number (lines of
 * source shown around a stack frame, not user data) and is named explicitly so
 * that a new numeric field does not inherit the exemption.
 */
describe("dataCollection", () => {
  const NUMERIC_FIELDS_ALLOWED = new Set(["frameContextLines"])

  function assertNothingCollected(value: unknown, path: string): void {
    if (typeof value === "boolean") {
      expect(value, `${path} must be false: SPEC §17 forbids collecting it`).toBe(false)
      return
    }
    if (Array.isArray(value)) {
      expect(value, `${path} must be empty: SPEC §17 forbids collecting it`).toEqual([])
      return
    }
    if (typeof value === "number") {
      expect(
        NUMERIC_FIELDS_ALLOWED.has(path),
        `${path} is a number this test has not vetted; decide whether it leaks user data`,
      ).toBe(true)
      return
    }
    if (value !== null && typeof value === "object") {
      // A nested group such as `httpHeaders`. An `{ allow: [...] }` or
      // `{ deny: [...] }` filter also lands here, and fails: its array is
      // non-empty for `allow`, and for `deny` the key is `deny`, which is not a
      // known group. Either way the console is collecting something.
      for (const [key, nested] of Object.entries(value)) {
        assertNothingCollected(nested, `${path}.${key}`)
      }
      return
    }
    throw new Error(`${path} has unexpected type ${typeof value}`)
  }

  it("collects no cookies, headers, bodies, query parameters or identity", () => {
    const options = clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: DSN })
    const dataCollection = options?.dataCollection
    expect(dataCollection, "dataCollection must be set explicitly").toBeDefined()

    for (const [key, value] of Object.entries(dataCollection as object)) {
      assertNothingCollected(value, key)
    }
  })

  it("names every category the installed SDK knows about", () => {
    // `resolveDataCollectionOptions()` in @sentry/core switches its baseline the
    // moment `dataCollection` is present: omitted fields then fall back to the
    // PERMISSIVE defaults (cookies on, headers on, all four body types), not to
    // the conservative `sendDefaultPii: false` set. So the danger is a field
    // left out — including one Sentry adds in a later version, which no
    // hand-written list here would ever notice.
    //
    // The expectation is therefore read out of the installed SDK rather than
    // restated. When this fails after a Sentry bump it is doing its job: a new
    // category exists, it defaults to collecting, and somebody has to decide
    // about it before the console starts sending it.
    const dataCollection = clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: DSN })?.dataCollection
    expect(Object.keys(dataCollection as object).sort()).toEqual(sdkDataCollectionCategories())
  })

  it("enables no Session Replay, which would record the DOM", () => {
    const options = clientSentryOptions({ NEXT_PUBLIC_SENTRY_DSN: DSN })
    expect(options).not.toHaveProperty("replaysSessionSampleRate")
    expect(options).not.toHaveProperty("replaysOnErrorSampleRate")
    expect(options).not.toHaveProperty("integrations")
  })
})
