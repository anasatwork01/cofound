/**
 * Contrast, checked rather than trusted. Task 0.11. SPEC 18.
 *
 * SPEC 18 sets one hard number for the console: "4.5:1 contrast minimum". This
 * test reads `palette.css` and `tokens.css`, resolves each semantic name
 * through its `var(--raw-...)` indirection to a real value, and measures every
 * pair that carries text with culori's `wcagContrast`.
 *
 * WHY IT IS WRITTEN THIS WAY
 *
 * The values in `palette.css` are provisional: SPEC 3.1 says the design system
 * comes from `docs/mockup.html`, and that file does not exist yet
 * (docs/open-questions.md Q0, task L.5). When the real values land they will be
 * dropped into `palette.css` and nothing else - and this test is what says
 * whether they pass. That makes the swap CHECKED rather than trusted: an
 * approved mockup with an amber that cannot carry white text is a fact somebody
 * needs to learn on the pull request, not from a user who cannot read a badge.
 *
 * For the same reason the test resolves the CSS rather than restating the
 * numbers. A table of hex values copied in here would assert against itself and
 * pass forever, no matter what the stylesheet said.
 */
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

import { wcagContrast } from "culori"
import { describe, expect, it } from "vitest"

const TOKEN_FILES = ["packages/ui/src/tokens/palette.css", "packages/ui/src/tokens/tokens.css"]

const MINIMUM_RATIO = 4.5

function declaredCustomProperties(): Map<string, string> {
  const declarations = new Map<string, string>()
  for (const file of TOKEN_FILES) {
    const source = readFileSync(fileURLToPath(new URL(`../../${file}`, import.meta.url)), "utf8")
    // Comments first, so a commented-out declaration is not read as live.
    const live = source.replace(/\/\*[\s\S]*?\*\//g, "")
    for (const match of live.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
      const name = match[1]
      const value = match[2]
      if (name === undefined || value === undefined) continue
      declarations.set(`--${name}`, value.trim().replace(/\s+/g, " "))
    }
  }
  return declarations
}

const DECLARATIONS = declaredCustomProperties()

/** Follow `var(--x)` indirection to a literal, the way a browser would. */
function resolve(name: string, seen: string[] = []): string {
  if (seen.includes(name)) {
    throw new Error(`${name} refers to itself: ${[...seen, name].join(" -> ")}`)
  }
  const value = DECLARATIONS.get(name)
  if (value === undefined) {
    throw new Error(
      `${name} is not declared in ${TOKEN_FILES.join(" or ")}. ` +
        "Every name the console is allowed to use has to exist in the token layer.",
    )
  }
  const reference = /^var\(\s*(--[\w-]+)\s*\)$/.exec(value)
  return reference?.[1] ? resolve(reference[1], [...seen, name]) : value
}

/**
 * The semantic surface, exactly as task 0.11 fixes it. Listed here so a rename
 * during the mockup swap fails loudly instead of leaving components pointing at
 * a name that no longer resolves.
 */
const REQUIRED_TOKENS = [
  "--color-agent",
  "--color-agent-ink",
  "--color-agent-soft",
  "--color-waiting",
  "--color-waiting-ink",
  "--color-waiting-soft",
  "--color-live",
  "--color-live-ink",
  "--color-live-soft",
  "--color-surface",
  "--color-surface-raised",
  "--color-surface-sunken",
  "--color-ink",
  "--color-ink-muted",
  "--color-border",
  "--color-focus",
  "--font-sans",
  "--font-mono",
  "--text-xs",
  "--text-sm",
  "--text-base",
  "--text-lg",
  "--text-xl",
  "--text-2xl",
  "--text-3xl",
  "--radius-sm",
  "--radius-md",
  "--radius-lg",
  "--space-1",
  "--space-2",
  "--space-3",
  "--space-4",
  "--space-5",
  "--space-6",
  "--space-7",
  "--space-8",
]

type Pair = {
  readonly foreground: string
  readonly background: string
  readonly why: string
  /**
   * The ratio this pair has to clear, when it is not SPEC 18's 4.5:1. Text
   * answers to 4.5:1; a BOUNDARY that is the only thing identifying a control
   * answers to WCAG 2.2 SC 1.4.11, which asks 3:1 of it. Both floors are
   * checked here, because both are checked against the mockup's own values the
   * day they land.
   */
  readonly floor?: number
}

/**
 * The pairs task 0.11 names. `-ink` is the foreground that goes ON that colour,
 * so each state's solid fill has to carry its own ink.
 */
const CONTRACT_PAIRS: readonly Pair[] = [
  { foreground: "--color-agent-ink", background: "--color-agent", why: "solid agent fill" },
  { foreground: "--color-waiting-ink", background: "--color-waiting", why: "solid waiting fill" },
  { foreground: "--color-live-ink", background: "--color-live", why: "solid live fill" },
  { foreground: "--color-ink", background: "--color-surface", why: "body text on the page" },
  { foreground: "--color-ink", background: "--color-surface-raised", why: "body text on a card" },
  { foreground: "--color-ink-muted", background: "--color-surface", why: "secondary text" },
]

/**
 * Pairs the contract does not enumerate but the console renders anyway. SPEC
 * 18's floor is a property of text on a background, not of a list, and a badge
 * nobody can read is a failure whether or not it was written down.
 */
const PAIRS_THE_CONSOLE_RENDERS: readonly Pair[] = [
  { foreground: "--color-ink", background: "--color-surface-sunken", why: "text in a well" },
  {
    foreground: "--color-ink-muted",
    background: "--color-surface-raised",
    why: "secondary text on a card",
  },
  {
    foreground: "--color-ink-muted",
    background: "--color-surface-sunken",
    why: "secondary text in a well",
  },
  { foreground: "--color-agent", background: "--color-agent-soft", why: "subtle agent badge" },
  {
    foreground: "--color-waiting",
    background: "--color-waiting-soft",
    why: "subtle waiting badge",
  },
  { foreground: "--color-live", background: "--color-live-soft", why: "subtle live badge" },
  { foreground: "--color-ink", background: "--color-agent-soft", why: "text on an agent tint" },
  { foreground: "--color-ink", background: "--color-waiting-soft", why: "text on a waiting tint" },
  { foreground: "--color-ink", background: "--color-live-soft", why: "text on a live tint" },
  { foreground: "--color-agent", background: "--color-surface", why: "agent text on the page" },
  { foreground: "--color-waiting", background: "--color-surface", why: "waiting text on the page" },
  { foreground: "--color-live", background: "--color-surface", why: "live text on the page" },
  {
    foreground: "--color-agent",
    background: "--color-surface-raised",
    why: "agent text on a card",
  },
  {
    foreground: "--color-waiting",
    background: "--color-surface-raised",
    why: "waiting text on a card",
  },
  { foreground: "--color-live", background: "--color-surface-raised", why: "live text on a card" },
  { foreground: "--color-focus", background: "--color-surface", why: "focus ring on the page" },
  {
    foreground: "--color-focus",
    background: "--color-surface-raised",
    why: "focus ring on a card",
  },
  /*
    The prompt box on /new is the product's entry screen, and its border is the
    only thing that says a box is there: the field and the page are both
    near-white (1.09:1 apart), so there is no fill contrast to fall back on.
    WCAG 2.2 SC 1.4.11 asks 3:1 of a boundary doing that job, against every
    surface the control can sit on - its own fill and the ground behind it.
    `--color-border` is 1.45:1 and fails; `--color-border-strong` exists for
    this. A mockup that arrives with a hairline grey for form controls gets
    caught here rather than by somebody who cannot find the input.
  */
  {
    foreground: "--color-border-strong",
    background: "--color-surface-raised",
    why: "a form control's boundary against its own fill",
    floor: 3,
  },
  {
    foreground: "--color-border-strong",
    background: "--color-surface",
    why: "a form control's boundary against the page behind it",
    floor: 3,
  },
]

function ratio(pair: Pair): number {
  return wcagContrast(resolve(pair.foreground), resolve(pair.background))
}

function describePair(pair: Pair): string {
  return `${pair.foreground} on ${pair.background} (${pair.why})`
}

describe("the token layer", () => {
  it.each(REQUIRED_TOKENS)("declares %s", (token) => {
    expect(() => resolve(token)).not.toThrow()
  })

  it("resolves every semantic colour through palette.css rather than naming one", () => {
    const direct: string[] = []
    for (const [name, value] of DECLARATIONS) {
      if (!name.startsWith("--color-")) continue
      if (["transparent", "currentColor", "inherit", "initial"].includes(value)) continue
      if (!/^var\(--raw-[\w-]+\)$/.test(value)) direct.push(`${name}: ${value}`)
    }
    expect(
      direct,
      "tokens.css must map roles onto raw names, never onto values. A colour written here is a " +
        "second file the mockup swap has to find:\n  " +
        direct.join("\n  "),
    ).toEqual([])
  })
})

describe("SPEC 18: 4.5:1 contrast minimum", () => {
  it.each(CONTRACT_PAIRS.map((p) => [describePair(p), p] as const))("%s", (_name, pair) => {
    const measured = ratio(pair)
    expect(
      measured,
      `${describePair(pair)} is ${measured.toFixed(2)}:1, below SPEC 18's ${MINIMUM_RATIO}:1.\n` +
        `  ${pair.foreground} = ${resolve(pair.foreground)}\n` +
        `  ${pair.background} = ${resolve(pair.background)}\n` +
        "Fix it in packages/ui/src/tokens/palette.css - it is the only file with the values.",
    ).toBeGreaterThanOrEqual(MINIMUM_RATIO)
  })

  it.each(PAIRS_THE_CONSOLE_RENDERS.map((p) => [describePair(p), p] as const))(
    "%s",
    (_name, pair) => {
      const measured = ratio(pair)
      const floor = pair.floor ?? MINIMUM_RATIO
      const rule =
        floor === MINIMUM_RATIO
          ? `SPEC 18's ${MINIMUM_RATIO}:1`
          : `WCAG 2.2 SC 1.4.11's ${floor}:1 for a boundary that identifies a control`
      expect(
        measured,
        `${describePair(pair)} is ${measured.toFixed(2)}:1, below ${rule}.\n` +
          `  ${pair.foreground} = ${resolve(pair.foreground)}\n` +
          `  ${pair.background} = ${resolve(pair.background)}\n` +
          "Fix it in packages/ui/src/tokens/palette.css - it is the only file with the values.",
      ).toBeGreaterThanOrEqual(floor)
    },
  )

  it("keeps the three states at one lightness, so they read as one pattern", () => {
    /* SPEC 18: "one learned pattern, not three". Equal contrast against the
       page is what makes an ads approval, a Stripe prompt and a DNS-pending
       domain feel like the same control rather than three designs. */
    const againstSurface = ["--color-agent", "--color-waiting", "--color-live"].map((state) =>
      wcagContrast(resolve(state), resolve("--color-surface")),
    )
    const spread = Math.max(...againstSurface) - Math.min(...againstSurface)
    expect(
      spread,
      `violet/amber/teal sit at ${againstSurface.map((r) => r.toFixed(2)).join(", ")}:1 against ` +
        "the page. More than a ratio apart and one state starts reading as louder than the others.",
    ).toBeLessThan(1)
  })
})
