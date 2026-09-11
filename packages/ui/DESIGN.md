# Console design tokens

SPEC §3.1: "The design system is defined by the approved mockup — read
`docs/mockup.html` and extract tokens from it rather than inventing new ones."

**`docs/mockup.html` is that file**, authored 2026-09-12 for task L.5. Everything
in `src/tokens/` is extracted from it. If the two ever disagree, the mockup is
right and this package is stale.

## Two tiers, and only one of them holds colours

```
tier 1  palette.css      raw values      the ONLY file with a colour literal
           ↓
tier 2  tokens.css       named by role   the names SPEC §18 fixes
           ↓
        components       tier 2 only, never tier 1
```

Tier 2's names are not a style choice. SPEC §18 fixes the semantics:

| Token       | Meaning (§18)      | Used identically for                                      |
| ----------- | ------------------ | --------------------------------------------------------- |
| `--agent`   | agent-owned action | an agent turn running, an ads approval the agent proposed |
| `--waiting` | waiting on you     | incomplete Stripe onboarding, a DNS-pending domain        |
| `--live`    | live               | a deployed version serving traffic, a healthy preview     |

§18's point is that these are **one learned pattern, not three**. So there is one
`Status` primitive, no ad-hoc badges, and its type makes a fourth state
unrepresentable.

## The rule that built the three states

They are anchored on **contrast**, not on lightness. Each solid sits at 4.6:1
against `--raw-grey-100`, the darkest surface any text sits on, and its lightness
falls wherever that puts it. Near-white ink then lands at 6.29 / 6.30 / 6.34 —
a spread of **0.05**, which is the only measurable form "one learned pattern"
has.

Pinning lightness is the obvious move and it does not work. Equal L is not equal
treatment: three colours pinned to the same lightness drift to a **0.90** spread
on exactly the pair a reader sees. Task 0.11's provisional palette made that
mistake, and measuring it is what caught it.

> **If a state colour ever has to move** — someone dislikes the amber, a hue gets
> rotated — re-anchor to 4.6:1 against `--raw-grey-100` and let lightness fall
> where it falls. **Never re-pin lightness.**

## What the guards enforce

A promise in a comment is not a promise. Three tests hold this:

1. **No component may name a colour.** `tests/console/tokens-quarantine.test.ts`
   fails on a hex literal, a Tailwind built-in colour utility, an arbitrary
   value (`bg-[red]` compiles to real CSS), a CSS shorthand, any of the 148
   named colours, or a direct `--raw-*` reference — anywhere outside
   `palette.css`. That last one matters most: a component pinned to
   `--raw-violet-600` would keep violet when the mockup remaps `--color-agent`,
   so a future change would miscolour silently instead of failing.
2. **Every pair clears its floor.** `tests/console/contrast.test.ts` resolves
   the real values through their `var()` indirection and asserts 4.5:1 for text,
   3:1 for control boundaries. This is what checked the extraction rather than
   trusting it.
3. **Every token name resolves.** `tests/console/shell-structure.test.ts` walks
   both `apps/console/src` and `packages/ui/src` and fails on a name the
   contract does not define — because an unknown Tailwind utility generates no
   rule at all, so `text-live-inks` would silently render inherited ink on a
   teal chip at 2.6:1 with nothing red anywhere.

## Things that are deliberate, and cost something if changed

- **The page ground is mid-light (L 92.5%), not near-white.** That is what lets
  three luminance planes — sunken, page, raised — carry the whole hierarchy with
  **no box-shadow anywhere in the product**, and what puts ink at 11.47:1 rather
  than the 19:1 that halates across a long session. Expect a pull toward a
  near-white default; it costs both properties.
- **There is no `#ffffff`.** `--raw-grey-0` is `#f9fbf8`, and it doubles as the
  ink on all three solids.
- **Radius encodes what a thing is**, not how new the design is: `0` for
  instruments and data surfaces you read, `--radius-sm` for anything you
  operate, `--radius-md` for anything that floats. `--radius-lg` is declared
  because the contract carries three and is deliberately unused — inventing a
  job for it would invent a fourth tier.
- **Mono is the voice of measurement and never of labels.** Credits, costs,
  durations, versions, positions, SHAs. Not captions, not eyebrows, not prose.
  The rule also delivers tabular figures by construction.
- **Italic has exactly one job**: marking a value as provisional, such as an
  estimated cost shown before a turn runs.
- **The fonts' fallback metrics are measured, not guessed.** `next/font`'s
  `adjustFontFallback` does not cover these families, so the metric-matched
  faces are authored in `apps/console/src/app/globals.css`. See
  `docs/verified.md`. **Re-measure if either family is replaced** — a stale
  `size-adjust` is worse than none.

## Changing the design

1. Change `docs/mockup.html` first. It is the source; this package is downstream.
2. Re-extract into `palette.css`. If anything outside that file needs to change,
   the quarantine test is telling you a component cheated.
3. Run the contrast test. If a pair drops below its floor, the design has to
   move — not the floor.
4. A **dark theme** is a second block of raw values plus `color-scheme: light
dark`, and a second set of pairs in `contrast.test.ts`. No component changes
   at all. Until those pairs are measured, dark would ship unmeasured.

**SPEC §21 decision 9 is answered by implication here.** §21.9 itself says the
mockup assumes semi-technical, and the mockup was authored on that assumption. A
purely non-technical audience would want a warmer palette and less code exposure
— a re-extraction plus a copy pass, not a rebuild. It is recorded as open to
overrule in `docs/open-questions.md`.
