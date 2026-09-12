import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"
import type { Page } from "@playwright/test"
import type { AxeResults, Result } from "axe-core"

import { routes } from "../../apps/console/src/lib/routes"

/**
 * SPEC §18, accessibility: "Keyboard-navigable throughout, visible focus rings,
 * `aria-live` for streaming regions and toasts, reduced-motion respected, 4.5:1
 * contrast minimum. Verify with axe in CI."
 *
 * This is the browser tier of that verification. It walks every route §18 fixes
 * and runs axe against the real document.
 *
 * WHY A BROWSER, AND WHY NOW (this gets asked, so it is answered here):
 *
 * The console today IS its chrome. Every rule that checks a document rather
 * than a component — `skip-link`, `bypass`, `landmark-one-main`,
 * `page-has-heading-one`, `document-title`, `html-has-lang`, `meta-viewport` —
 * is unreachable from a unit test, because a unit test renders a fragment into
 * a bare `<div>`: there is no `<html lang>`, no `<title>`, no skip link, and
 * every one of those rules goes *inapplicable* rather than failing. A 0.13 that
 * stopped at the unit tier would have verified the handful of components that
 * barely exist and skipped the twelve routes that do — which is exactly the
 * "green run that silently skipped half its rules" this task exists to prevent.
 *
 * Task 1.18 is not the place to start this. 1.18 depends on 1.16, deep in phase
 * 1, and until it lands §18's accessibility clause would be verified by
 * nothing. 1.18 INHERITS this config and adds its flows here as further spec
 * files, rather than standing up a second harness.
 */

/**
 * DERIVED from `@/lib/routes`, not copied from it.
 *
 * This list used to be hand-written, and the comment above it claimed that
 * `tests/console/shell-structure.test.ts` made that safe. It did not: that test
 * holds the route TABLE against the FILESYSTEM, and this file is neither. A
 * route added to both of those passed every check in the repository and was
 * simply never scanned by axe — silently, because a list that is not missing
 * anything and a list that is never compared look identical in a green run.
 *
 * So the table is the source. Every value in `routes` becomes a scanned URL,
 * function-valued entries are called with a demo slug, and a new route is
 * scanned the moment it is declared — or fails here, which is the same
 * protection arriving earlier.
 */
const PROJECT = "demo-project"

/**
 * The one thing a route value cannot carry: a query string.
 *
 * `/auth/callback` is the arrival that stands still only when it has a reason
 * code — bare, it redirects as soon as its effect runs, and scanning a document
 * on its way somewhere else scans nothing.
 */
const QUERY: Readonly<Record<string, string>> = {
  "/auth/callback": "?error=declined",
}

const ROUTES: ReadonlyArray<{ readonly name: string; readonly path: string }> = Object.entries(
  routes,
).map(([key, value]) => {
  const path = typeof value === "function" ? value(PROJECT) : value
  return { name: `${key} (${path})`, path: `${path}${QUERY[path] ?? ""}` }
})

/**
 * A derived list can go empty without anyone noticing — an import that resolves
 * to `{}` yields zero routes and a suite that passes by scanning nothing. This
 * is the same anti-vacuity floor the rest of this file applies to axe's rules.
 */
test("every route in the console's route table is scanned", () => {
  expect(ROUTES.length).toBeGreaterThanOrEqual(15)
  expect(ROUTES.map((route) => route.path)).toContain(`/p/${PROJECT}`)
  expect(new Set(ROUTES.map((route) => route.path)).size).toBe(ROUTES.length)
})

/**
 * `wcag22aa` is in this list for exactly one rule: `target-size`.
 *
 * That tag is NOT in task 0.13's brief, and it is here because the brief's
 * other instruction — "keep `target-size` enabled, a real browser can evaluate
 * geometry" — is not satisfiable without it. MEASURED against axe-core 4.13.0,
 * on `/p/[project]`, same disabled list:
 *
 *   tags without `wcag22aa` -> 87 rules in the run, target-size ABSENT
 *   tags with    `wcag22aa` -> 88 rules in the run, target-size PASSES, 9 nodes
 *
 * `target-size` carries `["cat.sensory-and-visual-cues","wcag22aa","wcag258"]`
 * and nothing else, and it is one of the rules axe ships switched off by
 * default — so without its tag it is not merely quiet, it is never registered.
 * It appears in NONE of axe's four result arrays, which means no audit of
 * `DISABLED` and no reading of the results would reveal that it had gone. Two
 * assertions below fail if that becomes true again.
 *
 * `wcag22aa` selects that one rule and no other in the whole 4.13 ruleset, so
 * this widens the run by exactly the thing a browser is here for: rendered
 * geometry, a 44x44 CSS pixel floor on interactive targets. The happy-dom unit
 * tier cannot evaluate it even in principle — happy-dom has no layout engine
 * and reports every element as 0x0.
 */
const TAGS = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa", "best-practice"]

/**
 * Three rules off, each for a reason, none of them "it was failing".
 *
 * `color-contrast` and `color-contrast-enhanced`: SPEC §18's 4.5:1 floor is
 * owned by `tests/console/contrast.test.ts`, which checks it at the TOKEN
 * layer — which is where a change to `docs/mockup.html` actually lands. A
 * browser-rendered sample of whichever pairs happen to be on screen today is a
 * strictly weaker check than every declared pair, and it would go quiet the
 * moment a screen stopped using a token rather than when the token broke.
 *
 * `link-in-text-block`: the same judgement one layer down. It asks whether a
 * link is distinguishable from surrounding text by more than colour, and
 * decides by sampling computed colours — so it passes or fails for reasons the
 * token contract already governs.
 *
 * Honest note on the second of the three: `color-contrast-enhanced` is tagged
 * `wcag2aaa`, which `TAGS` does not select, so disabling it changes nothing
 * today. It is named anyway so that adding `wcag2aaa` later cannot quietly
 * reintroduce a AAA contrast check that `contrast.test.ts` does not own.
 */
const DISABLED = ["color-contrast", "color-contrast-enhanced", "link-in-text-block"]

/**
 * The document-level rules. Every one of them is inapplicable to a unit test
 * and applicable to a page, which is the whole argument for this tier — so
 * every one of them is asserted to have actually run below.
 */
const CHROME_RULES = [
  "landmark-one-main",
  "page-has-heading-one",
  "bypass",
  "html-has-lang",
  "document-title",
  "skip-link",
] as const

/**
 * A floor on how many rules the run must actually register, whatever their
 * verdict. This catches the failure a green run cannot show you: a narrowed or
 * typo'd tag leaves axe evaluating a fraction of the ruleset and reporting no
 * violations, truthfully.
 *
 * 88 is MEASURED on axe-core 4.13.0, not derived — `TAGS` selects 100 rules,
 * and 12 of those never register: `color-contrast` and `link-in-text-block`
 * from `DISABLED`, and ten that axe ships switched off or marked experimental
 * (`hidden-content`, `p-as-heading`, `td-has-header` and the rest).
 * Sensitivity, same page:
 *
 *   TAGS as written            -> 88
 *   `wcag22aa` dropped         -> 87
 *   `best-practice` dropped    -> 61
 *   `["wcag2a"]` alone         -> 56
 *
 * So this bites on every narrowing, including the one-rule one. An axe upgrade
 * that ADDS rules passes; one that removes a rule fails here loudly, which is
 * the right way round — re-measure and change the number deliberately.
 */
const MIN_RULES_IN_RUN = 88

function scan(page: Page): AxeBuilder {
  return new AxeBuilder({ page }).withTags(TAGS).disableRules(DISABLED)
}

/** Every rule id axe actually registered for this run, whatever its verdict. */
function idsIn(...buckets: ReadonlyArray<readonly Result[]>): Set<string> {
  return new Set(buckets.flat().map((result) => result.id))
}

/**
 * `expect(violations).toEqual([])` prints a diff of whole axe result objects,
 * which is unreadable in a CI log. The assertion keeps that exact shape — the
 * empty array is the contract — and this supplies the line a human reads first.
 */
function describeViolations(violations: readonly Result[]): string {
  if (violations.length === 0) return "no axe violations"
  return violations
    .map((v) => {
      const where = v.nodes.map((n) => n.target.join(" ")).join(", ")
      return `${v.id} (${v.impact ?? "no impact"}): ${v.help} — ${where}`
    })
    .join("\n")
}

/**
 * Load a route and wait for it to stop moving.
 *
 * `document.fonts.ready` matters here specifically because `target-size` is
 * enabled: a button sized by its text label changes size when the webfont
 * swaps in, so scanning before fonts settle measures a layout no user sees.
 *
 * Deliberately NOT waiting on an `h1` or on the skip link. Those are the things
 * under test — waiting for one would turn its absence into a timeout in the
 * harness instead of a named axe violation, which is a worse failure message
 * for the same bug.
 */
async function open(page: Page, path: string): Promise<void> {
  const response = await page.goto(path)
  expect(response, `no response for ${path}`).not.toBeNull()
  expect(response?.status(), `${path} did not return a page`).toBeLessThan(400)
  await page.evaluate(() => document.fonts.ready)
}

test.describe("SPEC §18 accessibility: axe over every route", () => {
  for (const route of ROUTES) {
    test(`${route.name} has no axe violations`, async ({ page }) => {
      await open(page, route.path)

      const results: AxeResults = await scan(page).analyze()

      expect(results.violations, describeViolations(results.violations)).toEqual([])
    })
  }
})

/**
 * The anti-vacuity tier. Everything above can pass because the console is
 * accessible, or because axe was handed a page it could say nothing about —
 * and the two are indistinguishable from a green run. These assertions
 * separate them.
 *
 * The project route is the subject because it carries the most chrome: the root
 * layout's skip link, top bar, `<main>` and live region, plus the project
 * layout's tab nav and the page's own `<h1>`.
 */
test.describe("the axe run is not vacuous", () => {
  test("the document-level rules ran and reached a verdict", async ({ page }) => {
    await open(page, `/p/${PROJECT}`)
    const results = await scan(page).analyze()

    const reachedAVerdict = idsIn(results.passes, results.violations)
    const registered = idsIn(
      results.passes,
      results.violations,
      results.incomplete,
      results.inapplicable,
    )

    for (const rule of CHROME_RULES) {
      // Absent entirely: the tag list or `disableRules` stopped selecting it.
      expect(
        registered.has(rule),
        `${rule} was not in the axe run at all — check TAGS and DISABLED`,
      ).toBe(true)

      // Inapplicable: the rule ran and found nothing to judge. For these six
      // that means the shell is gone, not that the page is clean. `skip-link`
      // going inapplicable is the exact tell — it has no anchor to follow, so
      // the skip link has been removed or unhooked from `<main id="main">`.
      expect(
        reachedAVerdict.has(rule),
        `${rule} was inapplicable on /p/${PROJECT} — axe has stopped seeing the console shell`,
      ).toBe(true)
    }
  })

  test("target-size is registered, so the browser tier is earning its keep", async ({ page }) => {
    await open(page, `/p/${PROJECT}`)
    const results = await scan(page).analyze()

    const registered = idsIn(
      results.passes,
      results.violations,
      results.incomplete,
      results.inapplicable,
    )

    // Only "registered", not "reached a verdict": a page whose every target is
    // exempt (links inside a text block, say) legitimately leaves this
    // inapplicable. What must never happen is absence, which is what dropping
    // `wcag22aa` from TAGS would silently cause.
    expect(
      registered.has("target-size"),
      "target-size was not in the axe run — the wcag22aa tag has been dropped from TAGS, " +
        "and the one rule a real browser is here for is no longer running",
    ).toBe(true)
  })

  test("the tag list still selects the whole ruleset", async ({ page }) => {
    await open(page, `/p/${PROJECT}`)
    const results = await scan(page).analyze()

    const registered = idsIn(
      results.passes,
      results.violations,
      results.incomplete,
      results.inapplicable,
    )

    expect(
      registered.size,
      `axe registered ${registered.size} rules; a tag typo narrows the run and still reports green`,
    ).toBeGreaterThanOrEqual(MIN_RULES_IN_RUN)

    for (const rule of DISABLED) {
      expect(registered.has(rule), `${rule} is in DISABLED but still ran`).toBe(false)
    }
  })
})
