/**
 * Every route SPEC §18 fixes, in one place.
 *
 * §18 lists the route table as part of the contract, so it is written here once
 * and read by the navigation, by the pages and by the structural test in
 * `tests/console/shell-structure.test.ts`. A route added to the app without a
 * line here, or a line here without a route, fails that test — which is the
 * point: the table and the filesystem cannot drift apart quietly.
 */

/** A project slug reaches us from user input; never interpolate it raw. */
function slug(value: string): string {
  return encodeURIComponent(value)
}

export const routes = {
  /** SPEC §18: redirect to last project or /new. See `app/page.tsx` for the seam. */
  home: "/",
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
