import { act, cleanup, render, type RenderResult } from "@testing-library/react"
import axe, { type AxeResults, type RunOptions } from "axe-core"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ReactElement } from "react"

/**
 * `next/font/google` is a build-time transform, not a runtime module: called
 * outside `next build` it throws `Atkinson_Hyperlegible_Next is not a function`
 * and the root layout cannot be imported at all. Only the CSS variable names
 * are mocked, and they carry no accessible semantics — everything this file
 * asserts on is structure, name and role.
 */
vi.mock("@/app/fonts", () => ({
  sans: { variable: "--font-sans" },
  mono: { variable: "--font-mono" },
}))

/** `TabNav` marks the current tab from the pathname. */
const pathname = vi.hoisted(() => ({ current: "/" }))
vi.mock("next/navigation", () => ({ usePathname: () => pathname.current }))

import RootLayout from "@/app/layout"
import NewProjectPage from "@/app/new/page"
import BuilderPage from "@/app/p/[project]/page"
import FilesPage from "@/app/p/[project]/files/page"
import HistoryPage from "@/app/p/[project]/history/page"
import FeaturesPage from "@/app/p/[project]/features/page"
import ShipPage from "@/app/p/[project]/ship/page"
import AdsPage from "@/app/p/[project]/ads/page"
import SearchPage from "@/app/p/[project]/search/page"
import CreditsPage from "@/app/settings/credits/page"
import TeamPage from "@/app/settings/team/page"
import ConnectionsPage from "@/app/settings/connections/page"
import NotFound from "@/app/not-found"

import { EmptyState } from "@/components/empty-state"
import { LeaseBanner } from "@/components/lease-banner"
import { TabNav } from "@/components/tab-nav"
import { projectNav, routes, settingsNav } from "@/lib/routes"
import { useBuilderStore } from "@/store/builder-store"
import { CreditGauge, Status, TopBar, type MeterReading } from "@halyard/ui"

/**
 * SPEC §18: "Verify with axe in CI."
 *
 * Tier 1 of that: every §18 screen and every shared primitive, audited at unit
 * level so a regression fails in the second it is written rather than in a
 * browser job minutes later. Tier 2 (task 0.13's Playwright run) audits the
 * same screens with real CSS and real geometry, which is what this tier cannot
 * see.
 *
 * ---------------------------------------------------------------------------
 * WHY THIS CONFIGURATION, AND NOT THE DEFAULT ONE
 *
 * `best-practice` is NOT optional. Measured against `axe.getRules()`, the
 * whole `wcag2aa` tag is three rules — `color-contrast`, `meta-viewport`,
 * `valid-lang` — and every rule about landmarks, region coverage and heading
 * order carries `best-practice` and no WCAG tag at all. Dropping it removes
 * nine of the twenty-two rules `MUST_RUN` below insists on, in one edit, with
 * no other visible effect than a faster green run.
 *
 * The geometry rules are disabled at RUN level. `axe.configure()` does not
 * survive a tag-scoped `runOnly` — this is the only form that suppresses them.
 * They are suppressed because happy-dom has no layout: every element is 0x0
 * and every colour resolves to a default, so `target-size` is vacuous and
 * `color-contrast` cannot be answered. Contrast is not unowned —
 * `tests/console/contrast.test.ts` holds SPEC §18's 4.5:1 at the token layer,
 * which is where a design change actually lands.
 *
 * `wcag22aa` is deliberately absent: measured, the tag holds exactly one rule,
 * `target-size`, which is the geometry rule disabled above.
 */
const A11Y: RunOptions = {
  runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "best-practice"] },
  rules: {
    "color-contrast": { enabled: false },
    "color-contrast-enhanced": { enabled: false },
    "link-in-text-block": { enabled: false },
    "target-size": { enabled: false },
  },
}

/** The engine these numbers were measured against. */
const AXE_VERSION = "4.13.0"

type Fixture = {
  readonly name: string
  /** Set when the fixture is one of SPEC §18's routed screens. */
  readonly route?: string | undefined
  readonly element: ReactElement
  /**
   * Rendered on its own rather than inside the console shell. Only the chrome
   * needs it: the shell already mounts a top bar, and a second `<header>`
   * would be a real duplicate-banner violation rather than a fact about the
   * component under test.
   */
  readonly bare?: true | undefined
}

const UNKNOWN: MeterReading = { kind: "unknown" }
const MEASURED: MeterReading = { kind: "measured", used: 1240, allowance: 5000, hold: 180 }
const LOW: MeterReading = { kind: "measured", used: 4100, allowance: 5000, hold: 60 }
const OVERDRAWN: MeterReading = { kind: "measured", used: 5200, allowance: 5000, hold: 400 }

/**
 * The twelve screens SPEC §18 names, plus the primitives they are built from.
 *
 * The screen rows are checked against `@/lib/routes` below rather than trusted,
 * so a thirteenth route cannot be added without a row here.
 */
const FIXTURES: readonly Fixture[] = [
  { name: "/new", route: routes.newProject, element: <NewProjectPage /> },
  { name: "/p/[project]", route: routes.builder("demo"), element: <BuilderPage /> },
  { name: "/p/[project]/files", route: routes.files("demo"), element: <FilesPage /> },
  { name: "/p/[project]/history", route: routes.history("demo"), element: <HistoryPage /> },
  { name: "/p/[project]/features", route: routes.features("demo"), element: <FeaturesPage /> },
  { name: "/p/[project]/ship", route: routes.ship("demo"), element: <ShipPage /> },
  { name: "/p/[project]/ads", route: routes.ads("demo"), element: <AdsPage /> },
  { name: "/p/[project]/search", route: routes.search("demo"), element: <SearchPage /> },
  { name: "/settings/credits", route: routes.credits, element: <CreditsPage /> },
  { name: "/settings/team", route: routes.team, element: <TeamPage /> },
  { name: "/settings/connections", route: routes.connections, element: <ConnectionsPage /> },
  { name: "not-found", element: <NotFound /> },

  { name: "TopBar", bare: true, element: <TopBar org="Acme" project="demo" /> },
  {
    name: "TopBar with readings",
    bare: true,
    element: <TopBar org="Acme" project="demo" credits={{ build: MEASURED, runtime: LOW }} />,
  },
  { name: "CreditGauge unknown", element: <CreditGauge build={UNKNOWN} runtime={UNKNOWN} /> },
  { name: "CreditGauge measured", element: <CreditGauge build={MEASURED} runtime={MEASURED} /> },
  { name: "CreditGauge low", element: <CreditGauge build={MEASURED} runtime={LOW} /> },
  { name: "TabNav project", element: <TabNav label="Project" items={projectNav("demo")} /> },
  { name: "TabNav settings", element: <TabNav label="Settings" items={settingsNav} /> },
  {
    name: "Status",
    element: (
      <p>
        <Status state="agent">Writing the checkout page</Status>
        <Status state="waiting">Approve the ad budget</Status>
        <Status state="live" />
      </p>
    ),
  },
  {
    name: "EmptyState",
    element: (
      <EmptyState
        title="Nothing at this address"
        body="Check the address if you typed it, or start somewhere you know exists."
        action={{ href: routes.newProject, label: "Start a project" }}
      />
    ),
  },
  { name: "LeaseBanner", element: <LeaseBanner /> },
]

/**
 * The one fixture that is supposed to fail.
 *
 * Two planted defects, one structural and one naming: an `h1` followed by an
 * `h3`, and a link whose only content is decorative. If the configuration above
 * is ever neutered — `best-practice` dropped, a tag list narrowed, the whole
 * `runOnly` deleted — this stops reporting exactly these two and the build goes
 * red. An auditor that cannot be shown to bite is not an auditor.
 */
function Canary() {
  return (
    <main>
      <h1>Ship</h1>
      <h3>Domains</h3>
      <a href="/settings/connections">
        <svg aria-hidden="true" focusable="false" width="10" height="10" viewBox="0 0 12 12">
          <circle cx="6" cy="6" r="5" />
        </svg>
      </a>
    </main>
  )
}

/**
 * Fixtures are audited inside the real root layout unless they are the chrome.
 *
 * That is not decoration. Landmark and region rules only mean anything against
 * the structure the user actually meets: `region` asks whether all content sits
 * inside a landmark, and a component rendered bare into an empty body fails it
 * for a reason that has nothing to do with the component.
 *
 * `next/font/google` aside, this is the whole shell — skip link, top bar,
 * `<main>`, toast region — so a defect in the layout fails all twelve screens
 * rather than hiding in the one test that happens to look at it.
 */
function mount(fixture: Fixture): RenderResult {
  const tree = fixture.bare === true ? fixture.element : <RootLayout>{fixture.element}</RootLayout>
  return render(tree, { container: document.body, baseElement: document.body })
}

/**
 * `document.body`, never a container and never `document`.
 *
 * Measured on the shell tree, same markup, same options: 20 rules produce a
 * result from `body`, 18 from a container `<div>`, 26 from `document`.
 *
 * The container figure is the dangerous one. The two it silently drops are
 * `region` and `aria-hidden-body` — so an audit scoped to a container reports
 * clean on a page with no landmarks at all, which is the single thing SPEC
 * §18's structure rests on. `MUST_RUN` below is what turns that silence into a
 * failure.
 *
 * `document` is larger and still wrong: it reaches `<html>` and `<head>`, which
 * in a unit test belong to happy-dom rather than to the app. Measured, it
 * reports a `document-title` violation on every screen — Next sets the title
 * from each page's `metadata` export at build time, so the absence is an
 * artefact of the harness and auditing it would train everyone to ignore a
 * real rule.
 */
function audit(): Promise<AxeResults> {
  return axe.run(document.body, A11Y)
}

/** Rule ids that produced a result — as opposed to going inapplicable. */
function executedIn(results: AxeResults): readonly string[] {
  return [...results.passes, ...results.violations, ...results.incomplete].map((rule) => rule.id)
}

/** A violation list that says what broke and where, rather than `[]` vs `[…]`. */
function readable(results: AxeResults): readonly string[] {
  return results.violations.map(
    (violation) => `${violation.id}: ${violation.nodes.map((node) => node.target).join(" | ")}`,
  )
}

/** Accumulated across the per-fixture audits and asserted at the end. */
const executed = new Set<string>()
const audited: string[] = []

beforeEach(() => {
  pathname.current = routes.builder("demo")
  act(() => {
    useBuilderStore.getState().reset()
  })
  // `/new` reads `GET /templates` for real. Nothing in a unit test may reach
  // the network, and an unstubbed fetch here would try.
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ templates: [] }), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    ),
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe.each(FIXTURES)("$name", (fixture) => {
  it("has no accessibility violations", async () => {
    mount(fixture)
    const results = await audit()

    for (const id of executedIn(results)) executed.add(id)
    audited.push(fixture.name)

    expect(readable(results)).toEqual([])
    // A geometry rule that sneaks back past the disable returns as INCOMPLETE
    // rather than as a violation, so an empty `violations` alone would not
    // notice. So would a rule axe could not decide for any other reason: an
    // undecided rule is an unaudited one.
    expect(results.incomplete.map((rule) => rule.id)).toEqual([])
    expect(results.testEngine.version).toBe(AXE_VERSION)
  })

  it("has no duplicate element ids", () => {
    // axe cannot: `duplicate-id` is deprecated in axe-core 4.13 — its tags
    // are `wcag2a-obsolete` and `deprecated`, neither of which this run
    // selects — and it ships `enabled: false`, so it appears in neither the
    // results nor `inapplicable`. `duplicate-id-aria` survives it but only
    // covers ids an ARIA attribute points at; `htmlFor` and the skip link's
    // `#main` are outside it, and both resolve to the FIRST match in the
    // document, so a collision silently re-points them.
    mount(fixture)
    const ids = [...document.body.querySelectorAll("[id]")].map((element) => element.id)
    expect(ids).toEqual([...new Set(ids)])
  })
})

describe("the audit itself", () => {
  /**
   * Rules that must produce a result somewhere in the suite above.
   *
   * This is the anti-vacuity guard. An axe run over markup it cannot see
   * reports zero violations exactly as loudly as a clean one, and every way of
   * getting that wrong — a container root, a narrowed tag list, a rule renamed
   * or retired upstream — shows up here as a rule that stopped executing.
   *
   * Chosen so that both halves of the configuration are load-bearing:
   * `heading-order`, `region`, `landmark-unique`, `empty-heading` and the
   * `landmark-*` rules are tagged best-practice and NOTHING else, so dropping
   * that tag fails this list; `label`, `link-name`, `button-name`,
   * `list`/`listitem` and the `aria-*` rules come from the WCAG tags.
   */
  const MUST_RUN: readonly string[] = [
    // Reachable ONLY through `best-practice`. Measured against
    // `axe.getRules()`: none of these carries a `wcag2a`/`wcag2aa`/`wcag21*`
    // tag, so narrowing the tag list to WCAG drops all nine at once.
    "aria-allowed-role",
    "empty-heading",
    "heading-order",
    "landmark-banner-is-top-level",
    "landmark-main-is-top-level",
    "landmark-no-duplicate-banner",
    "landmark-no-duplicate-main",
    "landmark-unique",
    "region",
    // Reachable through the WCAG tags, and each one is some fixture above
    // doing its job: a nav list, a labelled textarea, a named button, a
    // progressbar with a name, an `aria-hidden` glyph.
    "aria-allowed-attr",
    "aria-hidden-focus",
    "aria-progressbar-name",
    "aria-required-attr",
    "aria-valid-attr",
    "aria-valid-attr-value",
    "button-name",
    "duplicate-id-aria",
    "label",
    "link-name",
    "list",
    "listitem",
    "nested-interactive",
  ]

  it("audited every fixture, which is what the list below is accumulated from", () => {
    expect(audited).toEqual(FIXTURES.map((fixture) => fixture.name))
  })

  it("actually ran the rules SPEC §18 is asking about", () => {
    expect(MUST_RUN.filter((id) => !executed.has(id))).toEqual([])
    // 30 rules produced a result across the fixtures above when this was
    // written. The floor is here so that a fixture quietly losing most of its
    // markup — a page that renders nothing, a mock that swallows a subtree —
    // shows up as lost coverage rather than as a faster green run.
    expect(executed.size).toBeGreaterThanOrEqual(MUST_RUN.length)
  })

  it("still bites when something is genuinely wrong", async () => {
    render(<Canary />, { container: document.body, baseElement: document.body })
    const results = await audit()
    expect(results.violations.map((violation) => violation.id).sort()).toEqual([
      "heading-order",
      "link-name",
    ])
  })

  it("covers every route SPEC §18 fixes, and nothing that is not one", () => {
    // `apps/console/src/app/screens.test.tsx` holds the same twelve screens.
    // It is not imported: measured, importing a test module re-registers its
    // 38 tests under THIS file (84 tests became 123), so the shared thing is
    // taken from `@/lib/routes` instead — the non-test module that already is
    // SPEC §18's route table, and that `tests/console/shell-structure.test.ts`
    // holds against the filesystem. A thirteenth route fails here until it has
    // a fixture.
    const covered = FIXTURES.flatMap((fixture) =>
      fixture.route === undefined ? [] : [fixture.route],
    )
    const fixed = [
      routes.newProject,
      ...projectNav("demo").map((item) => item.href),
      ...settingsNav.map((item) => item.href),
    ]
    expect([...covered].sort()).toEqual([...fixed].sort())
  })
})

describe("what axe cannot answer", () => {
  it("names every form control with a real label, never a placeholder", () => {
    // axe cannot: its `label` rule is satisfied by `non-empty-placeholder`,
    // which is one of the checks in its `any` list. So a field whose ONLY
    // accessible name is a placeholder passes, and `label-title-only` does not
    // rescue it either — axe-core 4.13.0's titleOnlyEvaluate is
    // `!labelText && !!(title || ariaDescribedBy)`, and a placeholder-only
    // field has neither.
    //
    // Measured on this repo: deleting `id={promptId}` from the /new textarea
    // leaves an orphan `<label for>` and an unlabelled control — a WCAG 2.1 A
    // failure on the primary input of the entry screen — and BOTH tiers stayed
    // fully green. This assertion is what turns that red.
    //
    // A placeholder is not a label: it disappears on focus, is not reliably
    // announced, and is the commonest way a form regresses without anyone
    // noticing.
    for (const fixture of FIXTURES) {
      cleanup()
      mount(fixture)
      const controls = [
        ...document.body.querySelectorAll<HTMLElement>("input, textarea, select"),
      ].filter((element) => element.getAttribute("type") !== "hidden")

      for (const control of controls) {
        const labelled =
          (control.id !== "" && document.querySelector(`label[for="${control.id}"]`) !== null) ||
          control.getAttribute("aria-label") !== null ||
          control.getAttribute("aria-labelledby") !== null ||
          control.closest("label") !== null

        expect(
          labelled,
          `${fixture.name}: <${control.tagName.toLowerCase()}> has no programmatic label. ` +
            `A placeholder does not count — axe's \`label\` rule accepts one, which is why ` +
            `this assertion exists rather than relying on the audit.`,
        ).toBe(true)
      }
    }
  })

  it("keeps aria-valuemin <= aria-valuenow <= aria-valuemax on every meter", () => {
    // axe cannot: no rule in any of these tags compares the three values.
    // `aria-valid-attr-value` only checks each is a number and
    // `aria-progressbar-name` only checks the accessible name, so an inverted
    // or out-of-range meter passes the whole audit. `credit-gauge.tsx` says in
    // its own comment that "an out-of-range progressbar is invalid ARIA" —
    // this is the check that was assumed to exist.
    mount({ name: "gauge", element: <CreditGauge build={MEASURED} runtime={LOW} /> })
    const bars = [...document.body.querySelectorAll('[role="progressbar"]')]
    expect(bars).toHaveLength(2)
    for (const bar of bars) {
      const min = Number(bar.getAttribute("aria-valuemin"))
      const now = Number(bar.getAttribute("aria-valuenow"))
      const max = Number(bar.getAttribute("aria-valuemax"))
      expect([min, now, max].every(Number.isFinite)).toBe(true)
      expect(min).toBeLessThanOrEqual(now)
      expect(now).toBeLessThanOrEqual(max)
    }
  })

  it("clamps an overdrawn meter to valuemax instead of reporting past it", () => {
    mount({ name: "gauge", element: <CreditGauge build={OVERDRAWN} runtime={MEASURED} /> })
    const bar = document.body.querySelector('[role="progressbar"]')
    expect(bar?.getAttribute("aria-valuenow")).toBe("5000")
    expect(bar?.getAttribute("aria-valuemax")).toBe("5000")
    // Clamped for ARIA, truthful in the name: the overdraw is still readable.
    expect(bar?.getAttribute("aria-label")).toBe("Build credits: 5,200 of 5,000 used, 400 on hold")
  })

  it("reports an unknown meter as unmeasured rather than as a meter reading zero", () => {
    mount({ name: "gauge", element: <CreditGauge build={UNKNOWN} runtime={UNKNOWN} /> })
    // No progressbar at all. `aria-valuenow="0"` would be a valid, in-range,
    // fully axe-clean way of telling every phase 0 user they are out of
    // credits, which is why this is asserted rather than left to the auditor.
    expect(document.body.querySelectorAll('[role="progressbar"]')).toHaveLength(0)
    expect(document.body.querySelectorAll("[aria-valuenow]")).toHaveLength(0)
    expect(document.body).toHaveTextContent("Not measured yet")
  })

  it("has a skip link, first in the document, whose target exists", () => {
    // axe cannot: measured, its own `skip-link` rule reports INAPPLICABLE
    // here. Its matcher is `isSkipLink(node) && isOffscreen(node)`, and
    // `isOffscreen` is a geometry question that happy-dom, which lays nothing
    // out, always answers no to. So the one thing a skip link has to do —
    // land on something — goes unchecked by the auditor on this tier.
    mount({ name: "builder", element: <BuilderPage /> })
    const skip = document.body.firstElementChild
    expect(skip?.tagName).toBe("A")
    expect(skip).toHaveTextContent("Skip to content")
    const href = skip?.getAttribute("href") ?? ""
    expect(href).toBe("#main")
    const target = document.getElementById(href.slice(1))
    expect(target).not.toBeNull()
    expect(target?.tagName).toBe("MAIN")
  })

  it("mounts the toast region empty, before there is anything to announce", () => {
    // axe cannot: it audits a snapshot. SPEC §18 requires the region to EXIST
    // before its content, because a live region created together with its text
    // is not reliably announced — that is a fact about two points in time, and
    // no snapshot rule can see it.
    mount({ name: "builder", element: <BuilderPage /> })
    const toast = document.getElementById("toast-region")
    expect(toast).not.toBeNull()
    expect(toast).toHaveAttribute("role", "status")
    expect(toast).toHaveAttribute("aria-live", "polite")
    expect(toast).toBeEmptyDOMElement()
  })

  it("keeps the credit gauge's low-credit region in the tree before it is low", () => {
    const view = mount({
      name: "gauge",
      bare: true,
      element: <CreditGauge build={MEASURED} runtime={MEASURED} />,
    })
    const regions = [...document.body.querySelectorAll('[aria-live="polite"]')]
    expect(regions).toHaveLength(2)
    for (const region of regions) expect(region).toBeEmptyDOMElement()

    const runtimeRegion = regions[1]
    view.rerender(<CreditGauge build={MEASURED} runtime={LOW} />)

    // The same node, still: a region that is replaced rather than filled
    // announces nothing, and is indistinguishable from this one in a snapshot.
    expect([...document.body.querySelectorAll('[aria-live="polite"]')][1]).toBe(runtimeRegion)
    expect(runtimeRegion).toHaveTextContent("Running low")
  })
})
