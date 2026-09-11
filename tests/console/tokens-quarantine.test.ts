/**
 * The colour quarantine. Task 0.11. SPEC 3.1, SPEC 18.
 *
 * `packages/ui/src/tokens/palette.css` is the only file in this repository
 * allowed to contain a colour literal. Everything else names a role -
 * `bg-agent`, `text-ink-muted`, `var(--color-surface-sunken)` - and the role
 * resolves, through `tokens.css`, to a raw value in that one file.
 *
 * WHY THIS TEST EXISTS
 *
 * SPEC 3.1 says the design system comes from `docs/mockup.html`: "extract
 * tokens from it rather than inventing new ones". That file does not exist and
 * never has (docs/open-questions.md Q0; committing it is task L.5). So the
 * values in `palette.css` are provisional, and the promise that replacing them
 * is a ONE-FILE change is the only thing making that acceptable.
 *
 * A promise like that decays the moment somebody writes a hex literal in a
 * component because it was quicker than adding a token. This test is what
 * turns the promise into a fact: it walks the console's source and fails if any
 * other file names a colour at all.
 *
 * WHAT COUNTS AS "NAMING A COLOUR"
 *
 * More than a hex. Each of these was reachable through an earlier version of
 * this file, and each was planted and watched to fail before the detector for
 * it was written:
 *
 *   var(--raw-violet-600)        the raw palette, read directly. The worst of
 *                                them: `--raw-*` is emitted on `:root`, so it
 *                                works, and a component pinned to it keeps
 *                                violet when the mockup remaps --color-agent.
 *                                The swap then MISCOLOURS instead of failing.
 *   bg-[red] / bg-(--raw-x)      Tailwind arbitrary values compile to real CSS.
 *   border: 1px solid red        a colour third in a shorthand, not first.
 *   color: dodgerblue            CSS has 148 named colours, not 40.
 *   color: Red                   colour names are case-insensitive.
 *   borderTopColor / boxShadow   the property list was never the whole list.
 *
 * The detectors run against the whole file rather than line by line, because
 * prettier splits a long declaration across lines - `backgroundImage:` on one,
 * its value on the next - and a detector that reads one line at a time sees
 * neither half.
 *
 * The detectors are themselves tested below - fed strings that must be flagged,
 * with the detector that has to catch each one, and strings that must not be
 * flagged - because a quarantine whose regex has quietly stopped matching is
 * worse than no quarantine, and passes just as green.
 */
import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative, sep } from "node:path"
import { fileURLToPath } from "node:url"

import { describe, expect, it } from "vitest"

const REPO_ROOT = fileURLToPath(new URL("../../", import.meta.url))

/**
 * THE one exemption, and the reason for it: this is the file the eventual
 * mockup swap replaces. Adding a second entry here means the swap is no longer
 * a one-file change, so it needs a reason written next to it, in this list.
 */
const COLOUR_LITERALS_ALLOWED_IN = ["packages/ui/src/tokens/palette.css"]

/**
 * The token layer, and the only place a `--raw-*` name may be READ: mapping
 * raw values onto roles is its entire job. `tokens.css` is scanned for
 * everything else.
 */
const RAW_NAMES_ALLOWED_UNDER = "packages/ui/src/tokens/"

/**
 * Where UI code lives. The rule is repo-wide, so `templates` is listed while it
 * still holds nothing but a README: the phase-2 app templates are exactly the
 * kind of code that arrives carrying a hex.
 *
 * `services/` and `agent/` hold no stylesheet and no markup. `tests/` is left
 * out for a different reason: this file has to contain colour literals - they
 * are the fixtures the detectors are tested against - so scanning it would make
 * the guard fail on its own test data.
 */
const SCAN_ROOTS = ["apps", "packages/ui", "templates"]

/** Build output and vendored code; nobody hand-edits these. */
const SKIP_DIRECTORIES = new Set([
  "node_modules",
  ".next",
  ".open-next",
  ".turbo",
  ".wrangler",
  "dist",
  "build",
  "coverage",
])

/** Generated from `packages/schema`; `make gen` owns its contents, not us. */
const SKIP_PATHS = ["packages/schema/gen"]

/** Text formats that can carry a colour. Markdown is documentation, not source. */
const SCANNED_EXTENSIONS = new Set([
  ".css",
  ".ts",
  ".tsx",
  ".js",
  ".jsx",
  ".mjs",
  ".cjs",
  ".mts",
  ".cts",
  ".svg",
  ".html",
])

/**
 * Tailwind's built-in palette. Our semantic names (agent, waiting, live, ink,
 * surface, border, focus) are deliberately not in this list, so `bg-agent` is
 * legal and `bg-violet-600` is not - even though they may currently resolve to
 * the same thing.
 */
const TAILWIND_COLOUR_NAMES = [
  "red",
  "orange",
  "amber",
  "yellow",
  "lime",
  "green",
  "emerald",
  "teal",
  "cyan",
  "sky",
  "blue",
  "indigo",
  "violet",
  "purple",
  "fuchsia",
  "pink",
  "rose",
  "slate",
  "gray",
  "zinc",
  "neutral",
  "stone",
]

/** Longest first, so `ring-offset-red-500` is not eaten by the `ring` branch. */
const TAILWIND_COLOUR_PREFIXES = [
  "bg",
  "text",
  "border",
  "ring",
  "ring-offset",
  "inset-ring",
  "outline",
  "fill",
  "stroke",
  "decoration",
  "shadow",
  "inset-shadow",
  "accent",
  "caret",
  "divide",
  "from",
  "via",
  "to",
  "placeholder",
].sort((a, b) => b.length - a.length)

const PREFIX_ALTERNATION = TAILWIND_COLOUR_PREFIXES.join("|")

/**
 * Contexts in which a `#` starts a fragment or a query, not a colour:
 * `href="#main"`, `url(/sprite.svg#icon)`, `https://example.com#anchor`.
 * Checked against the text to the LEFT of the match; the pattern cannot cross
 * whitespace, so it only ever reads the start of the same line.
 */
const URL_CONTEXT_BEFORE =
  /(?:https?:\/\/|\b(?:href|src|action|srcSet|xlink:href)\s*=\s*[{("'`]?|\burl\s*\()[^\s"'`)]*$/

/**
 * Every CSS named colour - all 148 of them, including the grey/gray spellings
 * and `rebeccapurple`. The list used to hold 40, which let `dodgerblue`,
 * `steelblue`, `whitesmoke` and about a hundred others through.
 *
 * Only flagged in a colour-valued property position, which is what keeps
 * ordinary prose - "the page ground is white", a variable called `gold` - out
 * of it. `transparent` and `currentColor` are deliberately absent: neither
 * names a colour, and `currentColor` is what an icon should inherit.
 */
const CSS_COLOUR_KEYWORDS = [
  "aliceblue",
  "antiquewhite",
  "aqua",
  "aquamarine",
  "azure",
  "beige",
  "bisque",
  "black",
  "blanchedalmond",
  "blue",
  "blueviolet",
  "brown",
  "burlywood",
  "cadetblue",
  "chartreuse",
  "chocolate",
  "coral",
  "cornflowerblue",
  "cornsilk",
  "crimson",
  "cyan",
  "darkblue",
  "darkcyan",
  "darkgoldenrod",
  "darkgray",
  "darkgreen",
  "darkgrey",
  "darkkhaki",
  "darkmagenta",
  "darkolivegreen",
  "darkorange",
  "darkorchid",
  "darkred",
  "darksalmon",
  "darkseagreen",
  "darkslateblue",
  "darkslategray",
  "darkslategrey",
  "darkturquoise",
  "darkviolet",
  "deeppink",
  "deepskyblue",
  "dimgray",
  "dimgrey",
  "dodgerblue",
  "firebrick",
  "floralwhite",
  "forestgreen",
  "fuchsia",
  "gainsboro",
  "ghostwhite",
  "gold",
  "goldenrod",
  "gray",
  "green",
  "greenyellow",
  "grey",
  "honeydew",
  "hotpink",
  "indianred",
  "indigo",
  "ivory",
  "khaki",
  "lavender",
  "lavenderblush",
  "lawngreen",
  "lemonchiffon",
  "lightblue",
  "lightcoral",
  "lightcyan",
  "lightgoldenrodyellow",
  "lightgray",
  "lightgreen",
  "lightgrey",
  "lightpink",
  "lightsalmon",
  "lightseagreen",
  "lightskyblue",
  "lightslategray",
  "lightslategrey",
  "lightsteelblue",
  "lightyellow",
  "lime",
  "limegreen",
  "linen",
  "magenta",
  "maroon",
  "mediumaquamarine",
  "mediumblue",
  "mediumorchid",
  "mediumpurple",
  "mediumseagreen",
  "mediumslateblue",
  "mediumspringgreen",
  "mediumturquoise",
  "mediumvioletred",
  "midnightblue",
  "mintcream",
  "mistyrose",
  "moccasin",
  "navajowhite",
  "navy",
  "oldlace",
  "olive",
  "olivedrab",
  "orange",
  "orangered",
  "orchid",
  "palegoldenrod",
  "palegreen",
  "paleturquoise",
  "palevioletred",
  "papayawhip",
  "peachpuff",
  "peru",
  "pink",
  "plum",
  "powderblue",
  "purple",
  "rebeccapurple",
  "red",
  "rosybrown",
  "royalblue",
  "saddlebrown",
  "salmon",
  "sandybrown",
  "seagreen",
  "seashell",
  "sienna",
  "silver",
  "skyblue",
  "slateblue",
  "slategray",
  "slategrey",
  "snow",
  "springgreen",
  "steelblue",
  "tan",
  "teal",
  "thistle",
  "tomato",
  "turquoise",
  "violet",
  "wheat",
  "white",
  "whitesmoke",
  "yellow",
  "yellowgreen",
]

/** Longest first, so `darkred` is not reported as `red`. */
const KEYWORD_ALTERNATION = [...CSS_COLOUR_KEYWORDS].sort((a, b) => b.length - a.length).join("|")

/**
 * Properties whose value can be a colour - including the SHORTHANDS, where the
 * colour arrives third in a list rather than first: `border: 1px solid red` is
 * the same violation as `border-color: red`, and an anchored detector saw
 * neither it nor `box-shadow: 0 1px 2px black`.
 *
 * camelCase spellings are derived rather than typed twice: `borderTopColor` in
 * a style object is the same property as `border-top-color` in a stylesheet,
 * and typing both by hand is how `borderTopColor` came to be missing.
 */
const COLOUR_VALUED_PROPERTY_NAMES = [
  "accent-color",
  "background",
  "background-color",
  "background-image",
  "border",
  "border-block-color",
  "border-bottom-color",
  "border-color",
  "border-inline-color",
  "border-left-color",
  "border-right-color",
  "border-top-color",
  "box-shadow",
  "caret-color",
  "color",
  "column-rule",
  "column-rule-color",
  "fill",
  "flood-color",
  "lighting-color",
  "outline",
  "outline-color",
  "scrollbar-color",
  "stop-color",
  "stroke",
  "text-decoration",
  "text-decoration-color",
  "text-emphasis-color",
  "text-shadow",
]

function camelCase(property: string): string {
  return property.replace(/-([a-z])/g, (_, letter: string) => letter.toUpperCase())
}

const COLOUR_VALUED_PROPERTIES = [
  ...new Set(COLOUR_VALUED_PROPERTY_NAMES.flatMap((name) => [name, camelCase(name)])),
].sort((a, b) => b.length - a.length)

const PROPERTY_ALTERNATION = COLOUR_VALUED_PROPERTIES.join("|")

/*
 * Shared sources, so the standalone detectors and the arbitrary-value content
 * check cannot drift apart. `-\[...]` compiles to real CSS in Tailwind v4
 * (`.bg-\[red\]{background-color:red}`), so its contents have to be held to
 * exactly the same rule as a declaration.
 */
const HEX_SOURCE = String.raw`(?<![\w&\\])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{4}|[0-9a-fA-F]{3})(?![\w-])`
const COLOUR_FUNCTION_SOURCE = String.raw`(?<![\w$.-])(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\s*\(`
const RAW_NAME_SOURCE = String.raw`--raw-\w[\w-]*`
/*
 * `.` and `/` join the lookbehind so a path segment - `url(/img/red.png)` -
 * does not read as a colour word.
 */
const KEYWORD_SOURCE = String.raw`(?<![\w\-./])(?:${KEYWORD_ALTERNATION})(?![\w-])`

const HEX = new RegExp(HEX_SOURCE)
const COLOUR_FUNCTION = new RegExp(COLOUR_FUNCTION_SOURCE, "i")
const RAW_NAME = new RegExp(RAW_NAME_SOURCE, "i")
const KEYWORD = new RegExp(KEYWORD_SOURCE, "i")

/**
 * Does this text name a colour, anywhere in it? Used on the inside of a
 * Tailwind arbitrary value, where there is no property to anchor to - the
 * square brackets ARE the declaration.
 *
 * `_` is Tailwind's space, so it is put back before matching: without that,
 * `shadow-[0_1px_2px_rgb(0_0_0/0.3)]` hides `rgb(` behind a word character.
 */
function namesAColour(text: string): boolean {
  const spaced = text.replace(/_/g, " ")
  return (
    HEX.test(spaced) ||
    COLOUR_FUNCTION.test(spaced) ||
    RAW_NAME.test(spaced) ||
    KEYWORD.test(spaced)
  )
}

type Detector = {
  readonly id: string
  readonly pattern: RegExp
  readonly fix: string
  /** Given the match and everything to its left, is this a false alarm? */
  readonly isFalsePositive?: (match: string, textBefore: string) => boolean
  /** Files this detector deliberately does not apply to, repo-relative. */
  readonly doesNotApplyTo?: (file: string) => boolean
}

const TOKEN_FIX = "add a semantic token in tokens.css backed by a raw value in palette.css"

const DETECTORS: readonly Detector[] = [
  {
    id: "hex-literal",
    /*
     * Exactly 3, 4, 6 or 8 hex digits, not part of a longer word. The length
     * restriction is also what keeps abbreviated git SHAs out: SPEC 18 wants
     * them "present but demoted to small monospace", and a 7-character SHA in a
     * comment is not a colour. `&` is excluded on the left so the HTML entity
     * `&#123;` does not read as `#123`.
     */
    pattern: new RegExp(HEX_SOURCE, "g"),
    fix: TOKEN_FIX,
    isFalsePositive: (_match, textBefore) => URL_CONTEXT_BEFORE.test(textBefore),
  },
  {
    id: "colour-function",
    /*
     * Lowercase and not preceded by a word character, `.` or `$`, so
     * `getColor(`, `d3.color(` and `theme.colors(` do not trip it. `color-mix()`
     * does not match either - it mixes variables rather than naming a colour,
     * which is exactly what we want people to do.
     */
    pattern: new RegExp(COLOUR_FUNCTION_SOURCE, "g"),
    fix: TOKEN_FIX,
  },
  {
    id: "raw-palette-name",
    /*
     * The raw palette is private to the token layer. This is the most damaging
     * escape of the lot and the least visible: `--raw-*` is emitted on `:root`
     * in the shipped CSS, so a component reading one renders correctly today,
     * and keeps its hue when the mockup remaps the role that was supposed to
     * own it. Nothing fails; the console is simply the wrong colour.
     *
     * `--raw-` followed by a name character, so prose about the `--raw-*`
     * namespace - as in packages/ui/src/index.ts - can still explain the rule.
     */
    pattern: new RegExp(RAW_NAME_SOURCE, "gi"),
    fix:
      "name the ROLE instead - bg-agent, var(--color-agent). --raw-* belongs to " +
      "packages/ui/src/tokens; if no role fits, add one in tokens.css",
    doesNotApplyTo: (file) => file.startsWith(RAW_NAMES_ALLOWED_UNDER),
  },
  {
    id: "tailwind-builtin-colour",
    pattern: new RegExp(
      String.raw`(?<![\w-])(?:${PREFIX_ALTERNATION})-(?:${TAILWIND_COLOUR_NAMES.join("|")})-(?:50|100|200|300|400|500|600|700|800|900|950)(?![\w-])`,
      "g",
    ),
    fix: "use the semantic utility for the role - bg-agent, bg-waiting, bg-live, bg-surface-raised, text-ink, text-ink-muted, border-border",
  },
  {
    id: "tailwind-builtin-colour",
    pattern: new RegExp(
      String.raw`(?<![\w-])(?:${PREFIX_ALTERNATION})-(?:white|black)(?![\w-])`,
      "g",
    ),
    fix: "white and black are roles here too - surface-raised, ink, and the -ink of each state",
  },
  {
    id: "tailwind-arbitrary-colour",
    /*
     * `bg-[red]`, `text-[#59519d]`, `bg-(--raw-violet-600)`. Tailwind v4
     * compiles both forms to real CSS, so they are declarations with different
     * punctuation - `bg-[red]` became `.bg-\[red\]{background-color:red}` in
     * the generated stylesheet, with no detector firing.
     *
     * The bracket itself is not the violation: `grid-cols-[auto_auto]` and
     * `transition-[width]` are how Tailwind expresses a one-off length. Only
     * the contents decide. The utility name is matched too, so the failure
     * report says `bg-[red]` rather than `-[red]`.
     */
    pattern: /(?<![\w-])[a-z][\w-]*(?:-\[[^\]]*\]|-\((--[\w-]+)\))/g,
    fix:
      "arbitrary values bypass the token layer: use a semantic utility, or add the token " +
      "and reach it as bg-(--color-<role>)",
    isFalsePositive: (match) => !namesAColour(match),
  },
  {
    id: "named-colour",
    /*
     * A colour-valued property, then anything up to the end of the declaration,
     * then a colour name. The gap is what catches the shorthands - `border: 1px
     * solid red`, `outline: 1px solid black`, `box-shadow: 0 1px 2px black` -
     * which an anchored pattern read as "not a colour in first position".
     *
     * `;`, `{`, `}` and quotes end the gap, so it cannot walk out of one
     * declaration and pair a property with the next value along. The single
     * optional quote after the colon is the JSX form, `color: "red"`.
     *
     * `i`, because CSS colour names are case-insensitive and `color: "Red"`
     * walked through a case-sensitive regex.
     */
    pattern: new RegExp(
      String.raw`(?<![\w-])(?:${PROPERTY_ALTERNATION})\s*[:=]\s*["'\`]?[^;{}"'\`]{0,160}?${KEYWORD_SOURCE}`,
      "gi",
    ),
    fix: `${TOKEN_FIX}; for an icon, \`currentColor\` inherits the role already set on the text`,
  },
]

type Violation = {
  readonly file: string
  readonly line: number
  readonly match: string
  readonly detector: string
  readonly fix: string
  readonly source: string
}

function walk(dir: string, out: string[]): string[] {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const entry of entries) {
    const full = join(dir, entry.name)
    const rel = relative(REPO_ROOT, full).split(sep).join("/")
    if (SKIP_PATHS.some((skip) => rel === skip || rel.startsWith(`${skip}/`))) continue
    if (entry.isDirectory()) {
      if (SKIP_DIRECTORIES.has(entry.name)) continue
      walk(full, out)
    } else if (entry.isFile()) {
      const dot = entry.name.lastIndexOf(".")
      if (dot > 0 && SCANNED_EXTENSIONS.has(entry.name.slice(dot))) out.push(rel)
    }
  }
  return out
}

function scannedFiles(): string[] {
  const found: string[] = []
  for (const root of SCAN_ROOTS) walk(join(REPO_ROOT, root), found)
  return found.filter((f) => !COLOUR_LITERALS_ALLOWED_IN.includes(f)).sort()
}

/** Every colour literal in one string, with the line it sits on. */
function findColourLiterals(source: string, file = "<memory>"): Violation[] {
  const violations: Violation[] = []
  for (const detector of DETECTORS) {
    if (detector.doesNotApplyTo?.(file)) continue
    const pattern = new RegExp(detector.pattern.source, detector.pattern.flags)
    let match: RegExpExecArray | null
    while ((match = pattern.exec(source)) !== null) {
      const text = match[0] ?? ""
      if (detector.isFalsePositive?.(text, source.slice(0, match.index))) continue
      const before = source.slice(0, match.index)
      const line = before.split("\n").length
      const lineStart = before.lastIndexOf("\n") + 1
      const lineEnd = source.indexOf("\n", match.index)
      violations.push({
        file,
        line,
        match: text,
        detector: detector.id,
        fix: detector.fix,
        source: source.slice(lineStart, lineEnd === -1 ? undefined : lineEnd).trim(),
      })
    }
  }
  return violations.sort((a, b) => a.line - b.line)
}

function report(violations: readonly Violation[]): string {
  const lines = violations.map(
    (v) =>
      `  ${v.file}:${v.line}  ${v.match}  [${v.detector}]\n      ${v.source}\n      fix: ${v.fix}`,
  )
  return [
    "",
    `${violations.length} colour literal${violations.length === 1 ? "" : "s"} outside palette.css:`,
    "",
    ...lines,
    "",
    "The fix is to add a semantic token, NOT to write the colour where it is used.",
    "In a test that exists to prove a colour cannot be passed, use a sentinel that is not a",
    "colour - the assertion is about the type, not the hue.",
    "",
    "  packages/ui/src/tokens/palette.css   raw values - the only file allowed a colour",
    "  packages/ui/src/tokens/tokens.css    maps raw values onto roles, exposes them to",
    "                                       Tailwind via @theme (bg-agent, text-ink, ...)",
    "",
    "A --raw-* name counts as a colour everywhere outside that directory. It resolves at",
    "runtime, so nothing looks wrong until the mockup remaps the role it was standing in",
    "for - and then the component keeps the old hue instead of failing.",
    "",
    "SPEC 3.1 says the design system comes from docs/mockup.html, which does not exist",
    "yet (docs/open-questions.md Q0, task L.5). Today's values are provisional, and the",
    "only reason that is acceptable is that swapping them is a one-file change. Every",
    "colour written anywhere else is a second file somebody has to find first.",
    "",
  ].join("\n")
}

describe("the detectors themselves", () => {
  /* A quarantine whose regexes have stopped matching passes exactly as green as
     one that works. These fix that, and double as the documentation of what
     counts as a colour.

     Each row names the detector that has to catch it. Asserting only "something
     flagged" let a new detector be added, never fire, and still look tested -
     the hex detector was catching several of these on its own. */
  const MUST_FLAG: ReadonlyArray<readonly [string, string, string]> = [
    ["six-digit hex", "  color: #59519d;", "hex-literal"],
    ["three-digit hex", "  color: #abc;", "hex-literal"],
    ["eight-digit hex with alpha", "  background: #59519dcc;", "hex-literal"],
    ["uppercase hex", "const c = '#F2F5F8'", "hex-literal"],
    ["rgb()", "  background: rgb(89 81 157);", "colour-function"],
    ["rgba()", "  background: rgba(89, 81, 157, 0.5);", "colour-function"],
    ["hsl()", "  color: hsl(286 40% 45%);", "colour-function"],
    ["oklch()", "  color: oklch(0.48 0.12 286);", "colour-function"],
    ["color()", "  color: color(display-p3 0.35 0.32 0.62);", "colour-function"],
    ["tailwind palette utility", '<div className="bg-blue-500" />', "tailwind-builtin-colour"],
    [
      "tailwind palette utility behind a variant",
      '<div className="hover:text-red-600" />',
      "tailwind-builtin-colour",
    ],
    [
      "tailwind palette utility with an offset prefix",
      '<div className="ring-offset-slate-200" />',
      "tailwind-builtin-colour",
    ],
    ["bare white", '<div className="bg-white" />', "tailwind-builtin-colour"],
    ["bare black", '<div className="text-black" />', "tailwind-builtin-colour"],
    ["a named colour in CSS", "  color: red;", "named-colour"],
    [
      "a named colour in a style object",
      '<div style={{ backgroundColor: "white" }} />',
      "named-colour",
    ],
    ["a named colour on an SVG attribute", '<path fill="black" />', "named-colour"],

    /* (a) The raw palette, reached directly. The worst one for the swap: it
       resolves at runtime, so it renders correctly right up until the mockup
       remaps the role, and then it silently keeps the old hue. */
    [
      "a raw palette name in a style attribute",
      '<div style={{ color: "var(--raw-violet-600)" }} />',
      "raw-palette-name",
    ],
    [
      "a raw palette name in a stylesheet",
      "  border-color: var(--raw-grey-200);",
      "raw-palette-name",
    ],
    [
      "a raw palette name behind a Tailwind arbitrary value",
      '<div className="bg-[var(--raw-violet-600)]" />',
      "raw-palette-name",
    ],

    /* (b) Tailwind arbitrary values, which compile to real CSS. */
    [
      "a named colour in an arbitrary value",
      '<div className="bg-[red]" />',
      "tailwind-arbitrary-colour",
    ],
    [
      "a hex in an arbitrary value",
      '<div className="text-[#59519d]" />',
      "tailwind-arbitrary-colour",
    ],
    [
      "a colour function in an arbitrary value, with Tailwind underscores",
      '<div className="shadow-[0_1px_2px_rgb(0_0_0/0.3)]" />',
      "tailwind-arbitrary-colour",
    ],
    [
      "a raw palette name in the CSS-variable shorthand",
      '<div className="bg-(--raw-violet-600)" />',
      "tailwind-arbitrary-colour",
    ],

    /* (c) A colour third in a shorthand rather than first. */
    ["a named colour in a border shorthand", "  border: 1px solid red;", "named-colour"],
    [
      "a named colour in an outline shorthand",
      '<div style={{ outline: "1px solid black" }} />',
      "named-colour",
    ],
    [
      "a named colour in a declaration prettier split across lines",
      '<div\n  style={{\n    backgroundImage:\n      "linear-gradient(to right, dodgerblue, navy)",\n  }}\n/>',
      "named-colour",
    ],

    /* (d) CSS has 148 named colours, not 40. */
    ["dodgerblue", "  color: dodgerblue;", "named-colour"],
    ["rebeccapurple", "  background: rebeccapurple;", "named-colour"],
    ["whitesmoke", '<div style={{ backgroundColor: "whitesmoke" }} />', "named-colour"],
    ["steelblue", "  fill: steelblue;", "named-colour"],

    /* (e) Colour names are case-insensitive. */
    ["a capitalised named colour", '<div style={{ color: "Red" }} />', "named-colour"],
    ["a shouted named colour", "  color: DODGERBLUE;", "named-colour"],

    /* (f) Properties the list had never heard of. */
    ["borderTopColor", '<div style={{ borderTopColor: "dodgerblue" }} />', "named-colour"],
    ["boxShadow", '<div style={{ boxShadow: "0 1px 2px black" }} />', "named-colour"],
    ["textShadow", '<div style={{ textShadow: "0 1px 0 white" }} />', "named-colour"],
    ["accentColor", '<div style={{ accentColor: "rebeccapurple" }} />', "named-colour"],
    ["columnRuleColor", "  column-rule-color: silver;", "named-colour"],
  ]

  const MUST_NOT_FLAG: ReadonlyArray<readonly [string, string]> = [
    ["our own semantic utilities", '<div className="bg-agent text-agent-ink border-border" />'],
    ["the muted ink role", '<p className="text-sm text-ink-muted">Waiting on you</p>'],
    ["our soft tints", '<span className="bg-waiting-soft text-ink" />'],
    ["a semantic custom property", "  background-color: var(--color-surface-sunken);"],
    ["an in-page anchor", '<a href="#main">Skip to content</a>'],
    ["a fragment inside url()", "  background: url(/sprite.svg#icon);"],
    ["an absolute URL with a fragment", "// see https://example.com/spec#abc for the rule"],
    ["an abbreviated SHA in a comment", "// SPEC 18: no git vocabulary - not commit c02b7ad"],
    ["a full SHA in a string", 'const sha = "c02b7ad9e1f4a6b8c3d5e7f9a1b3c5d7e9f1a3b5"'],
    ["an HTML entity", "const s = '&#123;'"],
    ["a capitalised helper call", "const v = getColor(state)"],
    ["a namespaced helper call", "const v = theme.color(state)"],
    [
      "color-mix, which mixes tokens rather than naming a colour",
      "  background: color-mix(in oklab, var(--color-agent) 12%, transparent);",
    ],
    ["a spacing utility", '<div className="p-4 gap-6 rounded-md text-xs" />'],
    ["a live-state utility that merely contains a palette word", '<div className="bg-live" />'],
    ["currentColor on an icon", '<path fill="currentColor" />'],
    ["a role passed as a prop", '<Status state="live" tone="agent" />'],
    [
      "prose that happens to name a colour",
      "// the provisional ground is an off-white, not a cream",
    ],
    [
      "prose that names the hues the tokens stand for",
      "// agent is violet, waiting is amber, live is teal - and nothing is ever red",
    ],
    ["a colour property set from a token", "  color: var(--color-ink-muted);"],

    /* Guards for the widened detectors. Each of these is the shape of a real
       line in this repository, and a false positive here would leave somebody
       deleting a correct declaration to get CI green. */
    [
      "an arbitrary value that is a length, not a colour",
      '<div className="grid-cols-[auto_auto_auto_auto] motion-safe:transition-[width]" />',
    ],
    [
      "the CSS-variable shorthand pointing at a role",
      '<div className="bg-(--color-agent-soft) text-(--color-ink)" />',
    ],
    ["a shorthand built from a token", "  border: 1px solid var(--color-border);"],
    [
      "a shadow built from a token",
      '<div style={{ boxShadow: "0 1px 2px var(--color-border)" }} />',
    ],
    ["the focus ring the token layer declares", "  outline: 2px solid var(--color-focus);"],
    ["color-scheme, which is not a colour", "  color-scheme: light;"],
    [
      "an identifier that begins with a colour-valued property name",
      '<div style={{ borderRadius: 4, borderTopWidth: 1, backgroundClip: "padding-box" }} />',
    ],
    [
      "the --raw-* namespace named as a namespace in documentation",
      " *   - anything from `./tokens/`, because `--raw-*` values are private to the",
    ],
    [
      "a gradient painted out of the semantic layer",
      '  backgroundImage: "repeating-linear-gradient(135deg, var(--color-agent) 0 2px, var(--color-agent-soft) 2px 5px)",',
    ],
  ]

  it("carries the whole CSS named-colour list, with no duplicates", () => {
    /* 148 is the count in CSS Color 4's named-colour table, including both
       spellings of grey and `rebeccapurple`. A shorter list is the bug this
       number exists to catch: at 40 names, `dodgerblue` was legal. */
    expect(new Set(CSS_COLOUR_KEYWORDS).size).toBe(148)
  })

  it.each(MUST_FLAG)("flags %s", (_name, source, detector) => {
    const violations = findColourLiterals(source)
    expect(
      violations.map((v) => v.detector),
      `nothing flagged this, or nothing did so as [${detector}]:\n  ${source}`,
    ).toContain(detector)
  })

  it.each(MUST_NOT_FLAG)("leaves %s alone", (_name, source) => {
    expect(findColourLiterals(source), report(findColourLiterals(source))).toEqual([])
  })

  it("lets the token layer, and only the token layer, read a raw value", () => {
    const mapping = "  --color-agent: var(--raw-violet-600);"
    expect(findColourLiterals(mapping, "packages/ui/src/tokens/tokens.css")).toEqual([])
    expect(
      findColourLiterals(mapping, "packages/ui/src/status/status.tsx").map((v) => v.detector),
    ).toContain("raw-palette-name")
  })
})

describe("the colour quarantine", () => {
  it("scans a non-trivial amount of source", () => {
    /* If the walk breaks - a renamed directory, a bad skip rule - it would find
       nothing and pass. This is the tripwire for that. */
    expect(scannedFiles().length).toBeGreaterThan(5)
  })

  it("scans every root that can hold UI code", () => {
    /* The rule is repo-wide. `templates` holds only a README today, so this is
       the assertion that says the root was chosen rather than forgotten when
       the phase-2 app templates land in it. */
    expect(SCAN_ROOTS).toContain("templates")
    for (const root of SCAN_ROOTS) expect(statSync(join(REPO_ROOT, root)).isDirectory()).toBe(true)
  })

  it("keeps palette.css the only file in the repository that names a colour", () => {
    const violations = scannedFiles().flatMap((file) =>
      findColourLiterals(readFileSync(join(REPO_ROOT, file), "utf8"), file),
    )
    expect(violations, report(violations)).toEqual([])
  })

  it.each(COLOUR_LITERALS_ALLOWED_IN)("%s exists and still holds the colours", (file) => {
    /* The exemption has to be load-bearing. If palette.css were emptied or
       moved, every other test here would still pass while the design system
       had quietly moved somewhere unchecked. */
    const path = join(REPO_ROOT, file)
    expect(statSync(path).isFile()).toBe(true)
    expect(findColourLiterals(readFileSync(path, "utf8"), file).length).toBeGreaterThan(5)
  })
})
