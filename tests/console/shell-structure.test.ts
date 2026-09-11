import { readFileSync, readdirSync, statSync } from "node:fs"
import { join, relative, sep } from "node:path"
import { describe, expect, it } from "vitest"

/**
 * Repo-wide assertions about the console shell.
 *
 * Chiefly SPEC §18's route table: the routes are a contract, and a screen
 * quietly added or renamed is a change to it, so the filesystem is compared
 * against the list §18 gives rather than trusted to match it.
 *
 * The colour quarantine itself lives next door in `tokens-quarantine.test.ts`
 * and the stylesheet wiring in `tokens-wiring.test.ts`; what is left here is
 * the one thing neither covers — that a component naming `text-ink-soft` or
 * `--space-9`, which would resolve to nothing and render invisibly rather than
 * failing, is caught.
 *
 * That last guard reads BOTH halves of the console's UI. It used to walk only
 * `apps/console/src`, which left `packages/ui/src` — where all three state
 * primitives live — unchecked: changing `text-live-ink` to `text-live-inks` in
 * `packages/ui/src/status/status.tsx` kept every test in this repository green
 * while `<Status state="live">` fell back to inherited ink on a teal fill, at
 * roughly 2.6:1 against SPEC §18's 4.5:1 floor. Nothing red on screen, nothing
 * red in CI. The route table and the `"use client"` rules below stay scoped to
 * `apps/console`, because they are properties of the app, not of the package.
 */

const REPO = join(import.meta.dirname, "..", "..")
const CONSOLE_SRC = join(REPO, "apps", "console", "src")
const UI_SRC = join(REPO, "packages", "ui", "src")
const APP = join(CONSOLE_SRC, "app")
/**
 * The token layer DECLARES names rather than using them, and legitimately holds
 * things no component may name — `--color-*: initial`, `--text-xs--line-height`,
 * `--color-transparent`. Its own side of the contract is checked by
 * `contrast.test.ts`, which fails if a required token stops resolving.
 */
const TOKEN_LAYER = join(UI_SRC, "tokens")

/** Copied from SPEC §18, verbatim, in its order. */
const SPEC_18_ROUTES = [
  "/",
  "/new",
  "/p/[project]",
  "/p/[project]/files",
  "/p/[project]/history",
  "/p/[project]/features",
  "/p/[project]/ship",
  "/p/[project]/ads",
  "/p/[project]/search",
  "/settings/credits",
  "/settings/team",
  "/settings/connections",
]

/**
 * The complete set of semantic names the token contract defines. Written out
 * rather than read from `tokens.css`, so a token quietly renamed there fails
 * here instead of silently redefining what the console is allowed to say.
 */
const COLOUR_TOKENS = [
  "agent",
  "agent-ink",
  "agent-soft",
  "waiting",
  "waiting-ink",
  "waiting-soft",
  "live",
  "live-ink",
  "live-soft",
  "surface",
  "surface-raised",
  "surface-sunken",
  "ink",
  "ink-muted",
  "border",
  /* Added with the /new prompt box, whose border is the only thing that says a
     control is there: SPEC §18's 4.5:1 is about text, and WCAG 2.2 SC 1.4.11
     asks 3:1 of a boundary doing that job, which `--color-border` misses. See
     the pair list in `contrast.test.ts`. */
  "border-strong",
  "focus",
]
const TOKEN_PROPERTIES = new Set([
  ...COLOUR_TOKENS.map((name) => `--color-${name}`),
  "--font-sans",
  "--font-mono",
  ...["xs", "sm", "base", "lg", "xl", "2xl", "3xl"].map((step) => `--text-${step}`),
  ...["sm", "md", "lg"].map((step) => `--radius-${step}`),
  ...[1, 2, 3, 4, 5, 6, 7, 8].map((step) => `--space-${step}`),
])

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    return statSync(path).isDirectory() ? walk(path) : [path]
  })
}

/** The app. Routes and client-boundary rules are about these files only. */
const ALL_FILES = walk(CONSOLE_SRC)
const SOURCE_FILES = ALL_FILES.filter(
  (path) => /\.(tsx|ts|css)$/.test(path) && !/\.test\.tsx?$/.test(path),
)

/**
 * Everything that USES a token: the app and the shared package, minus the token
 * layer itself. Test files are left out deliberately — this guard is about what
 * ships, and a test naming a token it expects to find is checked by the
 * component it renders.
 */
const TOKEN_USER_FILES = [...SOURCE_FILES, ...walk(UI_SRC)].filter(
  (path) =>
    /\.(tsx|ts|css)$/.test(path) &&
    !/\.test\.tsx?$/.test(path) &&
    !path.startsWith(TOKEN_LAYER + sep),
)

function read(path: string): string {
  return readFileSync(path, "utf8")
}

function routeOf(pagePath: string): string {
  const dir = relative(APP, pagePath).replace(/[/\\]page\.tsx$/, "")
  return dir === "page.tsx" ? "/" : `/${dir.split(/[/\\]/).join("/")}`
}

describe("SPEC §18 route table", () => {
  const pages = ALL_FILES.filter((path) => path.endsWith(`${"/"}page.tsx`))
  const routes = pages.map(routeOf).sort()

  it("has a screen for every route, and no route SPEC §18 does not list", () => {
    expect(routes).toEqual([...SPEC_18_ROUTES].sort())
  })

  it("puts the project screens under one layout, and settings under another", () => {
    expect(ALL_FILES).toContain(join(APP, "p", "[project]", "layout.tsx"))
    expect(ALL_FILES).toContain(join(APP, "settings", "layout.tsx"))
  })

  it("mounts the token layer and the top bar once, in the root layout", () => {
    const rootLayout = read(join(APP, "layout.tsx"))
    expect(rootLayout).toContain('import "./globals.css"')
    expect(rootLayout).toContain("@halyard/ui")
    expect(rootLayout).toContain("<TopBar />")
    // SPEC §18 wants aria-live for toasts, and a live region only announces
    // what is put into a region that already existed.
    expect(rootLayout).toContain('aria-live="polite"')
  })
})

describe("the token names the console uses", () => {
  it("scans the shared package as well as the app", () => {
    /* The tripwire for the hole this guard had: if the walk stops reaching
       packages/ui, the three state primitives go unchecked and every assertion
       below still passes. */
    const ui = TOKEN_USER_FILES.filter((path) => path.startsWith(UI_SRC + sep))
    expect(ui.map((path) => relative(REPO, path))).toContain(
      join("packages", "ui", "src", "status", "status.tsx"),
    )
    expect(ui.length).toBeGreaterThan(3)
  })

  it("names only tokens the contract defines", () => {
    const unknown = new Map<string, string>()
    for (const path of TOKEN_USER_FILES) {
      for (const match of read(path).matchAll(/--(?:color|font|text|radius|space)-[a-z0-9-]+/g)) {
        if (!TOKEN_PROPERTIES.has(match[0])) unknown.set(match[0], relative(REPO, path))
      }
      // The same names again, in the Tailwind utilities they generate. A
      // `text-ink-soft` typo resolves to nothing and renders an invisible
      // colour rather than failing, which is why this is worth asserting.
      for (const match of read(path).matchAll(
        /\b(?:bg|text|border|ring|fill|stroke|outline|divide|from|to|via)-([a-z][a-z0-9-]*)\b/g,
      )) {
        const value = match[1] ?? ""
        const family = value.split("-")[0] ?? ""
        /* `border` is in the list so `border-border` and `border-border-strong`
           are checked; `border-b`, `border-dashed` and the rest read as their
           own families and are none of this guard's business. */
        const families = ["agent", "waiting", "live", "surface", "ink", "border", "focus"]
        if (!families.includes(family)) continue
        if (!COLOUR_TOKENS.includes(value)) unknown.set(value, relative(REPO, path))
      }
    }
    expect(
      [...unknown.entries()],
      [
        "",
        "Token names that nothing defines, as [name, file]:",
        ...[...unknown.entries()].map(([name, file]) => `  ${name}  ${file}`),
        "",
        "None of these resolve, and none of them fail either: an unknown utility generates no",
        "rule at all, so the element keeps whatever it inherited. `text-live-inks` leaves a",
        "badge rendering --color-ink on bg-live — about 2.6:1, against SPEC §18's 4.5:1 floor —",
        "and looks deliberate. Fix the spelling, or add the token to packages/ui/src/tokens/",
        "tokens.css and to the contract list at the top of this file.",
        "",
      ].join("\n"),
    ).toEqual([])
  })

  it("has no tailwind config file, because v4 is CSS-first", () => {
    const consoleDir = readdirSync(join(REPO, "apps", "console"))
    expect(consoleDir.filter((entry) => entry.startsWith("tailwind.config"))).toEqual([])
  })
})

describe('"use client"', () => {
  const clientFiles = SOURCE_FILES.filter((path) => /^["']use client["']/.test(read(path).trim()))

  it("is only where state, effects or event handlers actually are", () => {
    const withoutReason = clientFiles.filter((path) => {
      const source = read(path)
      // error.tsx is the exception Next imposes rather than one we chose.
      if (path.endsWith(`${"/"}error.tsx`)) return false
      return !/use(?:State|Effect|Id|Query|Ref|Pathname)|useBuilderStore|on[A-Z]/.test(source)
    })
    expect(withoutReason.map((path) => relative(REPO, path))).toEqual([])
  })

  it("is never on a page or layout, which are server components by default", () => {
    const routeFiles = clientFiles.filter((path) => /[/\\](page|layout)\.tsx$/.test(path))
    expect(routeFiles.map((path) => relative(REPO, path))).toEqual([])
  })
})
