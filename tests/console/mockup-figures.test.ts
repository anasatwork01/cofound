/**
 * The approved mockup's own numbers, checked rather than trusted. SPEC 3.1.
 *
 * `docs/mockup.html` is the authority: SPEC 3.1 says the design system is
 * defined by it, and the file itself says "if a number here disagrees with the
 * console, the console is wrong". That makes a stale figure in it worse than a
 * stale figure anywhere else - it is the thing everything else is checked
 * against.
 *
 * And it went stale exactly once already. The 2026-09-12 warming replaced the
 * surface ramp and the three state values in `palette.css`, and the mockup's
 * prose went on describing a green-grey ramp at H~131, "no #ffffff anywhere",
 * and ink at 11.47:1 - none of which were true any more. Nothing failed,
 * because nothing read the file.
 *
 * This test reads it. Every figure the document prints is RE-DERIVED from the
 * document's own `:root` block with culori; no expected value is typed here, so
 * the test cannot drift into agreeing with a wrong number.
 */
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

import { oklch, parse, wcagContrast } from "culori"
import { describe, expect, it } from "vitest"

const read = (p: string) =>
  readFileSync(fileURLToPath(new URL(`../../${p}`, import.meta.url)), "utf8")
const SRC = read("docs/mockup.html")
const PALETTE = read("packages/ui/src/tokens/palette.css")

/** The mockup's `:root`, which is the extraction source for `palette.css`. */
const ROOT: Record<string, string> = (() => {
  const open = SRC.indexOf(":root {")
  const block = SRC.slice(open, SRC.indexOf("/* ===", open))
  const out: Record<string, string> = {}
  for (const m of block.matchAll(/--(raw-[\w-]+)\s*:\s*(#[0-9a-fA-F]{3,8})\s*;/g)) {
    out[m[1]!] = m[2]!.toLowerCase()
  }
  return out
})()

const ratio = (fg: string, bg: string) => wcagContrast(ROOT[fg]!, ROOT[bg]!).toFixed(2)
const STATES = ["raw-violet-600", "raw-amber-600", "raw-teal-600"] as const
const SURFACES = ["raw-grey-50", "raw-grey-0", "raw-grey-100"] as const

describe("docs/mockup.html declares the palette the console actually ships", () => {
  it("declares every raw colour palette.css does, at the same value", () => {
    expect(Object.keys(ROOT).length).toBeGreaterThan(0)
    for (const [name, hex] of Object.entries(ROOT)) {
      const found = new RegExp(`--${name}\\s*:\\s*(#[0-9a-fA-F]{3,8})\\s*;`).exec(PALETTE)
      expect(found?.[1]?.toLowerCase(), `--${name} is in the mockup but not in palette.css`).toBe(
        hex,
      )
    }
  })
})

/** Each swatch row: the chip's token, the hex column and the OKLCH column. */
const SWATCH_ROWS = [
  ...SRC.matchAll(
    /<span class="swatch" style="background: var\(--(raw-[\w-]+)\)"><\/span>[\s\S]{0,200}?<code>([\w-]+)<\/code>[\s\S]{0,200}?<td class="num">(#[0-9a-f]{6})<\/td>\s*<td class="num">([\d.]+)% \.(\d{3}) (&mdash;|\d+)<\/td>/g,
  ),
]

describe("the colour table prints what :root actually holds", () => {
  it("parses every row", () => {
    expect(SWATCH_ROWS.length).toBe(Object.keys(ROOT).length)
  })

  it.each(SWATCH_ROWS.map((m) => [m[2]!, m] as const))("%s", (_name, m) => {
    const [, rawName, codeName, hex, l, c, h] = m
    expect(rawName, "the swatch chip is wired to a different token than the row names").toBe(
      `raw-${codeName}`,
    )
    const live = ROOT[rawName!]!
    expect(hex, `${codeName}: the hex column disagrees with :root`).toBe(live)
    const o = oklch(parse(live))!
    expect((o.l * 100).toFixed(1), `${codeName}: OKLCH lightness`).toBe(l)
    expect(o.c.toFixed(3).slice(2), `${codeName}: OKLCH chroma`).toBe(c)
    if (h === "&mdash;")
      expect(o.c, `${codeName} prints no hue, so it must be achromatic`).toBeLessThan(0.0005)
    else expect(String(Math.round(o.h ?? 0)), `${codeName}: OKLCH hue`).toBe(h)
  })
})

/**
 * The "Measured contrast" table, parsed by ROW LABEL rather than searched for
 * as a substring. Substring search is why an earlier draft of this test did not
 * bite: several figures appear more than once in the document, so a `toContain`
 * on a stale row still found the live copy elsewhere and passed. Each row is
 * now bound to the computation it claims to report.
 */
function tableRows(heading: string): Map<string, string[]> {
  const from = SRC.indexOf(heading)
  expect(from, `the document no longer has a "${heading}" section`).toBeGreaterThan(-1)
  const table = SRC.slice(from, SRC.indexOf("</table>", from))
  const rows = new Map<string, string[]>()
  for (const tr of table.matchAll(/<tr>([\s\S]*?)<\/tr>/g)) {
    const cells = [...tr[1]!.matchAll(/<td[^>]*>([\s\S]*?)<\/td>/g)].map((c) =>
      c[1]!
        .replace(/<[^>]+>/g, "")
        .replace(/\s+/g, " ")
        .trim(),
    )
    if (cells.length >= 2) rows.set(cells[0]!, cells.slice(1))
  }
  return rows
}

describe("every contrast figure the document prints is the measured one", () => {
  const MEASURED = tableRows("<h3>Measured contrast</h3>")

  const expected: ReadonlyArray<readonly [string, string]> = [
    ["White ink on violet / amber / teal", STATES.map((s) => ratio("raw-grey-0", s)).join(" · ")],
    ["Ink on page / raised / sunken", SURFACES.map((b) => ratio("raw-grey-900", b)).join(" · ")],
    [
      "Muted ink on page / raised / sunken",
      SURFACES.map((b) => ratio("raw-grey-600", b)).join(" · "),
    ],
    [
      "Each solid as text on page / raised",
      STATES.map((s) => ratio(s, "raw-grey-50")).join(" · ") +
        " / " +
        STATES.map((s) => ratio(s, "raw-grey-0")).join(" · "),
    ],
    [
      "Each solid on its own tint",
      STATES.map((s) => ratio(s, s.replace("-600", "-50"))).join(" · "),
    ],
    [
      "Ink on each of the three tints",
      STATES.map((s) => ratio("raw-grey-900", s.replace("-600", "-50"))).join(" · "),
    ],
    [
      "Control edge on raised / page / sunken",
      (["raw-grey-0", "raw-grey-50", "raw-grey-100"] as const)
        .map((b) => ratio("raw-grey-500", b))
        .join(" · "),
    ],
    ["Hold hatch on unspent track", ratio("raw-violet-600", "raw-grey-100")],
  ]

  it("reports exactly the rows this test knows how to derive", () => {
    expect([...MEASURED.keys()].sort()).toEqual(expected.map(([label]) => label).sort())
  })

  it.each(expected)("%s", (label, figures) => {
    expect(MEASURED.get(label)?.[0], `the "${label}" row disagrees with the measured value`).toBe(
      figures,
    )
  })

  it("floors every row at a real WCAG threshold it actually clears", () => {
    for (const [label, cells] of MEASURED) {
      const floor = Number(cells[1])
      expect([3, 4.5], `"${label}" cites a floor that is not a WCAG threshold`).toContain(floor)
      for (const n of cells[0]!.split(/[·/]/)) {
        expect(
          Number(n.trim()),
          `"${label}" prints ${n.trim()}, below the ${floor}:1 it claims`,
        ).toBeGreaterThanOrEqual(floor)
      }
    }
  })

  /** The equalisation table: three per-state cells plus the spread. */
  it("prints the measured spread of white ink across the three states", () => {
    const rows = tableRows("<h2>How these values were built</h2>")
    const anchored = [...rows.entries()].find(([label]) => label.includes("this system"))
    expect(anchored, "the equalisation table no longer has a row for this system").toBeDefined()
    const [, cells] = anchored!
    const inks = STATES.map((s) => wcagContrast(ROOT["raw-grey-0"]!, ROOT[s]!))
    expect(cells.slice(0, 3), "the per-state cells disagree with the measured ratios").toEqual(
      inks.map((r) => r.toFixed(2)),
    )
    const spread = (Math.max(...inks) - Math.min(...inks)).toFixed(2)
    expect(cells[3], "the spread cell disagrees with the measured spread").toBe(spread)
  })

  /** Ratios quoted in prose rather than in a table. */
  const scalars: ReadonlyArray<readonly [string, string]> = [
    ["the focus gap, page against violet", ratio("raw-grey-50", "raw-violet-600")],
    ["violet on teal, the pair the hatch avoids", ratio("raw-violet-600", "raw-teal-600")],
    ["raised against page", ratio("raw-grey-0", "raw-grey-50")],
    ["page against sunken", ratio("raw-grey-50", "raw-grey-100")],
    ["ink on raised", ratio("raw-grey-900", "raw-grey-0")],
  ]

  it.each(scalars)("%s is quoted as measured", (_what, figure) => {
    expect(SRC, `the measured ratio ${figure} appears nowhere in the document`).toContain(figure)
  })
})
