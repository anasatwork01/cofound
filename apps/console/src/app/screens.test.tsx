import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ReactElement } from "react"

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

import { renderWithQuery } from "@/test-utils"

/**
 * Every screen SPEC §18 names, rendered, and held to §18's copy rules.
 *
 * These are all plain synchronous components on purpose. Next documents async
 * Server Components as unsupported in unit tests and the failure is silent —
 * the component renders nothing and an "is not present" assertion passes
 * vacuously — so the two layouts that must be async (`/p/[project]`, because
 * `params` is a promise) are covered by task 1.18's end-to-end test instead.
 */

type Screen = { name: string; heading: string; element: ReactElement; needsQuery?: boolean }

const SCREENS: Screen[] = [
  { name: "/new", heading: "Start something", element: <NewProjectPage />, needsQuery: true },
  { name: "/p/[project]", heading: "Builder", element: <BuilderPage /> },
  { name: "/p/[project]/files", heading: "Files", element: <FilesPage /> },
  { name: "/p/[project]/history", heading: "History", element: <HistoryPage /> },
  { name: "/p/[project]/features", heading: "Features", element: <FeaturesPage /> },
  { name: "/p/[project]/ship", heading: "Ship", element: <ShipPage /> },
  { name: "/p/[project]/ads", heading: "Ads", element: <AdsPage /> },
  { name: "/p/[project]/search", heading: "Search", element: <SearchPage /> },
  { name: "/settings/credits", heading: "Credits", element: <CreditsPage /> },
  { name: "/settings/team", heading: "Team", element: <TeamPage /> },
  { name: "/settings/connections", heading: "Connections", element: <ConnectionsPage /> },
  { name: "not-found", heading: "That page is not here", element: <NotFound /> },
]

/** SPEC §18: errors and empty states never apologise. */
const APOLOGY = /\bsorry\b|\bapolog|\boops\b|\bunfortunately\b|\bwe're having trouble\b/i

/** SPEC §18: no git vocabulary in primary surfaces. */
const GIT_VOCABULARY =
  /\bcommit(s|ted)?\b|\brepositor|\brepo\b|\bbranch(es)?\b|\bsha\b|\brebase|\bgit\b/i

/** A tell of generated design, and banned by this task's brief. */
const ARROW_IN_A_BUTTON = /→/

beforeEach(() => {
  // /new queries `GET /templates` and `GET /auth/session` for real. Nothing in
  // a unit test may reach the network, and an unstubbed fetch here would try.
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

function show(screenUnderTest: Screen): string {
  if (screenUnderTest.needsQuery === true) renderWithQuery(screenUnderTest.element)
  else render(screenUnderTest.element)
  return document.body.textContent ?? ""
}

describe.each(SCREENS)("$name", (screenUnderTest) => {
  it("opens with exactly one heading, and it is the screen's own name", () => {
    show(screenUnderTest)
    const headings = screen.getAllByRole("heading", { level: 1 })
    expect(headings).toHaveLength(1)
    expect(headings[0]).toHaveTextContent(screenUnderTest.heading)
  })

  it("says something worth reading rather than TODO or lorem", () => {
    const text = show(screenUnderTest)
    expect(text.toLowerCase()).not.toMatch(/\btodo\b|lorem ipsum|coming soon|under construction/)
    expect(text.length).toBeGreaterThan(80)
  })

  it("follows §18's copy rules", () => {
    const text = show(screenUnderTest)
    expect(text).not.toMatch(APOLOGY)
    expect(text).not.toMatch(GIT_VOCABULARY)
    expect(text).not.toMatch(ARROW_IN_A_BUTTON)
    expect(text).not.toMatch(/[A-Z]{4,}/) // no ALL-CAPS eyebrow labels
  })
})

describe("empty states", () => {
  it("invite the reader to do the next thing", () => {
    // Every project and settings screen that is empty says what will fill it
    // and how, rather than reporting that it is empty.
    for (const screenUnderTest of SCREENS) {
      const text = show(screenUnderTest)
      expect(text, `${screenUnderTest.name} has no invitation`).toMatch(
        // `textContent` runs the heading straight into the body with no
        // separator, so a leading word boundary would not fire on the first word.
        /(ask|describe|connect|invite|publish|start|check|choose)\b/i,
      )
      cleanup()
    }
  })

  it("sends the ads screen somewhere real to connect an account", () => {
    show({ name: "ads", heading: "Ads", element: <AdsPage /> })
    expect(screen.getByRole("link", { name: "Connect an ad account" })).toHaveAttribute(
      "href",
      "/settings/connections",
    )
  })
})
