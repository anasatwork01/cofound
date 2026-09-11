# Console design tokens — how this is structured, and what to do when the mockup lands

SPEC §3.1: "The design system is defined by the approved mockup — read
`docs/mockup.html` and extract tokens from it rather than inventing new ones."

**`docs/mockup.html` does not exist.** It was never supplied, is not in this
repository's history, and `docs/open-questions.md` Q0 records it as
unrecoverable and owned by a human (task L.5). Task 0.11 still had to ship the
console shell, so this file explains the shape that decision took.

## The rule this obeys

CLAUDE.md is explicit that the approved mockup wins for the console, and the
`frontend-design` skill agrees: "where the brief pins down a visual direction,
follow it exactly." A missing mockup is not permission to invent a visual
identity for the console — a distinctive one would be **harder** to replace
than a plain one, because identity leaks out of a palette and into component
structure, density and motion.

So 0.11 ships the token layer's **structure**, which SPEC §18 fully specifies,
and treats every **value** as provisional and replaceable.

## Two tiers, and only one of them is provisional

```
tier 1  palette.css      raw values      PROVISIONAL — the mockup replaces this file
           ↓
tier 2  semantic tokens  named by role   PERMANENT — SPEC §18 fixes these names
           ↓
        components       reference tier 2 only, never tier 1
```

Tier 2's names are not a style choice. SPEC §18 fixes the semantics:

| Token       | Meaning (§18)      | Used identically for                                      |
| ----------- | ------------------ | --------------------------------------------------------- |
| `--agent`   | agent-owned action | an agent turn running, an ads approval the agent proposed |
| `--waiting` | waiting on you     | incomplete Stripe onboarding, a DNS-pending domain        |
| `--live`    | live               | a deployed version serving traffic, a healthy preview     |

§18's point is that these are **one learned pattern, not three**: the same
three states for an ads approval, Stripe onboarding and a pending domain. So
there is one `Status` primitive and no ad-hoc badges.

## What "provisional" is enforced to mean

A promise in a comment is not a promise. Two tests hold this:

1. **No component may name a colour.** A test greps the console and `packages/ui`
   for hex literals, `rgb(`, `hsl(` and Tailwind's built-in colour classes
   (`bg-blue-500` and friends), and fails on any hit outside `palette.css`. So
   swapping the mockup's values in is a one-file change by construction, not by
   good intentions.
2. **The provisional pairs already meet §18's contrast floor.** §18 requires
   4.5:1 minimum. The test computes the real WCAG ratio for every
   foreground/background pair the semantic layer defines, so when the mockup's
   values land the same test says whether they pass — the swap is checked, not
   trusted.

## The provisional values, and why they look like this

Deliberately quiet, and chosen to be _uncharacteristic_ rather than
characteristic:

- **A neutral near-grey ground, not a warm cream.** Warm cream (#F4F1EA) with a
  terracotta accent is the single commonest generated-design tell, and it would
  read as a design decision rather than a placeholder.
- **System font stack, no webfont.** A typeface is the loudest identity choice
  in an interface and the mockup owns it. A system stack is also zero bytes,
  which matters on Workers.
- **Monospace only for SHAs.** The skill advises against monospace for small
  data labels; §18 explicitly requires "SHAs present but demoted to small
  monospace". The brief wins.
- **Violet / amber / teal hues are taken from §18's own words**, at
  restrained saturation. The hues are specified; the exact values are not.

## Where the boldness goes

One element carries this interface, and SPEC §18 names it rather than leaving it
to taste: the credit gauge sits in the top bar on **every** screen, showing
build and runtime as separate bars with the active hold drawn as **hatching** —
"deliberate: users need to see burn while causing it."

That is the most characteristic thing in this product's world. You are watching
an agent spend your money in real time, and the hatched hold is the only widget
in the console that shows a commitment which has not yet resolved. Everything
around it stays quiet.

The hatching is a CSS `repeating-linear-gradient` over the bar's filled
portion, not an image, so it inherits the semantic colour and survives the
token swap. It is also not motion: it is a static texture, because a moving
hold would draw the eye continuously while a user is trying to work.

## When `docs/mockup.html` arrives

1. Extract its values into `palette.css`. Nothing else should need to change —
   and if something does, that is the quarantine test telling you a component
   cheated.
2. Run the contrast test. It will say whether the mockup's own pairs clear
   4.5:1; if they do not, that is a conversation with whoever approved it, not a
   thing to quietly adjust.
3. Re-check the type scale and density against the mockup, which are the two
   axes a palette swap does not carry.
4. Delete the "provisional" banner from `palette.css` and this section.
5. SPEC §21 decision 9 — whether the console targets primarily non-technical
   users — changes the visual register (warmth, density, how much code is
   exposed). It is still open. The mockup presumably answers it; if the mockup
   arrives without that decision being made, ask before extracting.
