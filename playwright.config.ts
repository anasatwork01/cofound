import { tmpdir } from "node:os"
import { join } from "node:path"
import { defineConfig } from "@playwright/test"

/**
 * End-to-end configuration, currently serving task 0.13's axe tier.
 *
 * SPEC §18 ends with "Verify with axe in CI", and the half of §18 that axe can
 * only see in a real browser is the chrome: `skip-link`, `bypass`,
 * `landmark-one-main`, `page-has-heading-one`, `document-title`,
 * `html-has-lang`, `meta-viewport`. None of those is reachable from a unit
 * test, because none of them is a component — they are properties of a whole
 * rendered document. Today the console IS its chrome, so a 0.13 that stopped
 * at the unit tier would have checked the four components that barely exist
 * and skipped the twelve routes that do.
 *
 * This is deliberately NOT deferred to task 1.18: 1.18 depends on 1.16, deep
 * in phase 1, and until then §18's accessibility clause would be verified by
 * nothing. 1.18 INHERITS this config and adds its flows to `tests/e2e/` as
 * further spec files; it does not build a second harness.
 */

/**
 * `next start` against the `next build` output the js job already produces.
 *
 * Deliberately NOT `opennextjs-cloudflare preview`. That is a second full
 * build on top of `next build`, and it changes nothing axe can observe — the
 * DOM a Worker serves is the DOM Node serves. The OpenNext build is worth
 * running in CI for its own sake (it can fail on things `next build` passes),
 * so the js job runs `cf:build`; it just does not belong in front of a browser.
 */
const PORT = 3100
const HOST = "127.0.0.1"
const BASE_URL = `http://${HOST}:${PORT}`

export default defineConfig({
  testDir: "./tests/e2e",

  /**
   * Artifacts go to the OS temp directory rather than `test-results/` in the
   * working tree. `make lint-js` is `prettier --check .` with no
   * `--ignore-path`, so a Playwright run that dropped `.last-run.json` into the
   * repo would make the next lint fail on a file nobody wrote. Traces from a
   * failing local run land here; `pnpm exec playwright show-trace <path>`.
   */
  outputDir: join(tmpdir(), "halyard-e2e-artifacts"),

  /**
   * A stray `test.only` would silently reduce this suite to one route and still
   * report green. In CI that is a failure, not a convenience.
   */
  forbidOnly: !!process.env.CI,

  /**
   * No retries. An axe result is a pure function of the rendered DOM; a rule
   * that fails once fails every time. Retrying would only convert a real
   * regression into a flake to be ignored.
   */
  retries: 0,

  reporter: process.env.CI ? [["github"], ["list"]] : [["list"]],

  use: {
    baseURL: BASE_URL,

    /**
     * `browserName` with no `channel`, and that absence is load-bearing. CI
     * installs with `playwright install --only-shell chromium`, which puts
     * `chromium-headless-shell` on disk and nothing else. Playwright 1.63
     * resolves a headless chromium launch to that shell on its own — verified
     * by launching against a cache holding only the shell. `channel: "chromium"`
     * would demand the full build the cache does not hold, and
     * `channel: "chrome"` a Google Chrome nobody installed.
     */
    browserName: "chromium",

    /**
     * Pinned rather than inherited, because `target-size` measures geometry and
     * geometry depends on the viewport. A default that moved between Playwright
     * releases would move this suite's verdict with it.
     */
    viewport: { width: 1280, height: 720 },

    trace: "retain-on-failure",
  },

  webServer: {
    command: "pnpm --filter @halyard/console start",
    /**
     * `/` is a 307 to `/new` (SPEC §18's "redirect to last project or /new"),
     * so readiness is probed against a route that actually returns a page.
     */
    url: `${BASE_URL}/new`,
    env: {
      PORT: String(PORT),
      HOSTNAME: HOST,
      /**
       * Empty, not merely unset. Task 0.13 wires `@sentry/nextjs` into this
       * console, and an empty DSN is how you tell `Sentry.init` to do nothing.
       * Stating it here means a repository or organisation variable cannot
       * quietly start reporting CI page loads into a real Sentry project — and
       * because `NEXT_PUBLIC_*` is inlined at build time, the same value has to
       * be in force for the build that this server serves.
       */
      SENTRY_DSN: "",
      NEXT_PUBLIC_SENTRY_DSN: "",
    },
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    // Next's own startup output, so a server that refuses to boot says why in
    // the CI log instead of only timing out.
    stdout: "pipe",
    stderr: "pipe",
  },
})
