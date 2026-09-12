/**
 * Every route the console has, in one place.
 *
 * SPEC §18 lists its route table as part of the contract, so it is written here
 * once and read by the navigation, by the pages and by the structural test in
 * `tests/console/shell-structure.test.ts`. A route added to the app without a
 * line here, or a line here without a route, fails that test — which is the
 * point: the table and the filesystem cannot drift apart quietly.
 *
 * ---------------------------------------------------------------------------
 * THE THREE ROUTES §18 DOES NOT LIST, AND WHY EACH IS HERE (task 0.14)
 *
 * §18's table is the product's screens, and every one of them assumes a user
 * who is already signed in and already in an organisation. Phase 0's acceptance
 * criterion is the sentence before that: "a user can sign up, create an org,
 * sign in, and see an empty project list". Those steps need somewhere to
 * happen, and none of them is a screen §18 forgot — they are the door, the
 * doormat and the first room.
 *
 *   /signin         The door. §18 begins after it.
 *
 *   /auth/callback  NOT a screen anyone navigates to, and its address is not
 *                   ours to choose: `services/api/internal/auth/magiclink.go`
 *                   builds every emailed link as
 *                   `CONSOLE_ORIGIN + "/auth/callback?token=..."`, and the
 *                   Google OAuth handler redirects the browser to the same
 *                   path. Rename it here and every sign-in link in every inbox
 *                   lands on a 404.
 *
 *   /orgs/new       Creating your first organisation. It cannot live under
 *                   `/settings/*`: those screens are scoped to an org you are
 *                   already in, which is precisely what this does not have.
 *
 * A fourth route is NOT here on purpose: there is no `/projects`. The project
 * list is on `/new`, which is where §18 already puts the two things you do with
 * it — start another, or open one you started.
 */

/** A project slug reaches us from user input; never interpolate it raw. */
function slug(value: string): string {
  return encodeURIComponent(value)
}

export const routes = {
  /** SPEC §18: redirect to last project or /new. See `app/page.tsx` for the seam. */
  home: "/",
  /** Not a §18 screen. See the note at the top of this file. */
  signIn: "/signin",
  /** Not a §18 screen, and its spelling belongs to the API. See the note above. */
  authCallback: "/auth/callback",
  /** Not a §18 screen. See the note above. */
  newOrg: "/orgs/new",
  newProject: "/new",
  builder: (project: string) => `/p/${slug(project)}`,
  files: (project: string) => `/p/${slug(project)}/files`,
  history: (project: string) => `/p/${slug(project)}/history`,
  features: (project: string) => `/p/${slug(project)}/features`,
  ship: (project: string) => `/p/${slug(project)}/ship`,
  ads: (project: string) => `/p/${slug(project)}/ads`,
  search: (project: string) => `/p/${slug(project)}/search`,
  credits: "/settings/credits",
  team: "/settings/team",
  connections: "/settings/connections",
} as const

export type NavItem = {
  readonly href: string
  /**
   * The screen's name. It is the same word in the nav, in the <h1> and in any
   * link that points at the screen — SPEC §18: an action keeps its name through
   * the whole flow.
   */
  readonly label: string
}

/**
 * The project-scoped nav, in SPEC §18's order. Builder first because it is the
 * screen the product is for; everything else is a view onto what it produced.
 */
export function projectNav(project: string): readonly NavItem[] {
  return [
    { href: routes.builder(project), label: "Builder" },
    { href: routes.files(project), label: "Files" },
    { href: routes.history(project), label: "History" },
    { href: routes.features(project), label: "Features" },
    { href: routes.ship(project), label: "Ship" },
    { href: routes.ads(project), label: "Ads" },
    { href: routes.search(project), label: "Search" },
  ]
}

export const settingsNav: readonly NavItem[] = [
  { href: routes.credits, label: "Credits" },
  { href: routes.team, label: "Team" },
  { href: routes.connections, label: "Connections" },
]

/**
 * Which nav item a pathname is "on". A prefix match would light up Builder for
 * every project screen, because `/p/x` prefixes `/p/x/files`, so the builder
 * matches exactly and the rest match themselves or anything below them (a file
 * path lives under `/files/...` once task 2.7 lands).
 */
export function isCurrent(pathname: string, href: string, items: readonly NavItem[]): boolean {
  const exact = items.some((item) => item.href === pathname)
  if (exact) return href === pathname
  const candidates = items.filter((item) => pathname.startsWith(`${item.href}/`))
  const longest = candidates.reduce<string | null>(
    (best, item) => (best === null || item.href.length > best.length ? item.href : best),
    null,
  )
  return longest === href
}
