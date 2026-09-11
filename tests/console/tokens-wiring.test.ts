/**
 * The one join in the token layer that fails silently. Task 0.11. SPEC 3.1.
 *
 * `packages/ui/src/tokens/tokens.css` cannot `@import "tailwindcss"` itself.
 * `tailwindcss` is a devDependency of `apps/console`, pnpm's strict layout does
 * not expose it to `packages/ui`, and Tailwind resolves a nested `@import`
 * relative to the file that wrote it:
 *
 *     Can't resolve 'tailwindcss' in 'packages/ui/src/tokens'
 *
 * Making it resolvable means adding a dependency to `packages/ui`, which task
 * 0.11 rules out. So the app's entry stylesheet has to import Tailwind first
 * and the token layer second, and that ordering is an unwritten agreement
 * between two packages - the kind that gets broken by a tidy-up six months from
 * now, with no error, just a console that has lost its `@theme`.
 *
 * This test writes the agreement down.
 */
import { readdirSync, readFileSync } from "node:fs"
import { join, relative, sep } from "node:path"
import { fileURLToPath } from "node:url"

import { describe, expect, it } from "vitest"

const REPO_ROOT = fileURLToPath(new URL("../../", import.meta.url))
const CONSOLE_ROOT = join(REPO_ROOT, "apps/console")
const TOKENS_CSS = "packages/ui/src/tokens/tokens.css"
const TOKENS_IMPORT = "@halyard/ui/tokens.css"

function stylesheets(dir: string, out: string[] = []): string[] {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue
    const full = join(dir, entry.name)
    if (entry.isDirectory()) stylesheets(full, out)
    else if (entry.name.endsWith(".css")) out.push(relative(REPO_ROOT, full).split(sep).join("/"))
  }
  return out
}

const entryStylesheets = stylesheets(CONSOLE_ROOT).filter((file) =>
  readFileSync(join(REPO_ROOT, file), "utf8").includes(TOKENS_IMPORT),
)

describe("the console reaches the token layer", () => {
  it("imports the token layer from at least one stylesheet", () => {
    expect(
      entryStylesheets,
      `No stylesheet in apps/console imports "${TOKENS_IMPORT}". Without it the console has no ` +
        "design system at all: no @theme, so no bg-agent, no text-ink, no spacing scale.",
    ).not.toEqual([])
  })

  it.each(entryStylesheets)("%s imports tailwindcss before the token layer", (file) => {
    const source = readFileSync(join(REPO_ROOT, file), "utf8")
    const tailwind = source.indexOf('@import "tailwindcss"')
    const tokens = source.indexOf(TOKENS_IMPORT)
    expect(
      tailwind,
      `${file} imports ${TOKENS_IMPORT} but never imports tailwindcss. ${TOKENS_CSS} cannot do ` +
        "it - see the comment at the top of that file - so this stylesheet must.",
    ).toBeGreaterThanOrEqual(0)
    expect(
      tailwind,
      `${file} imports ${TOKENS_IMPORT} before tailwindcss. Tailwind's layer order is declared ` +
        'by its own import, so `@import "tailwindcss"` has to come first.',
    ).toBeLessThan(tokens)
  })

  it("does not let tokens.css import tailwindcss, because it cannot", () => {
    const source = readFileSync(join(REPO_ROOT, TOKENS_CSS), "utf8").replace(
      /\/\*[\s\S]*?\*\//g,
      "",
    )
    expect(
      source.includes('@import "tailwindcss"'),
      `${TOKENS_CSS} imports tailwindcss. That cannot resolve from packages/ui under pnpm, and ` +
        "the build fails with: Can't resolve 'tailwindcss' in 'packages/ui/src/tokens'.",
    ).toBe(false)
    expect(source).toContain("@theme")
    expect(source).toContain('@import "./palette.css"')
  })
})
