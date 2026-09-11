# packages/ui

Shared React components for the console.

**Tokens are provisional.** SPEC §3.1 says to extract them from
`docs/mockup.html`, and that file does not exist (task L.5, `docs/open-questions.md`
Q0). `src/tokens/palette.css` is the only file in the repository permitted to
contain a colour literal, and that is enforced by
`tests/console/tokens-quarantine.test.ts` so the eventual swap is a one-file
change. **Read `DESIGN.md` before touching anything in `src/tokens/`.**

Semantic colour has exactly three states (SPEC §18): violet = agent-owned
action, amber = waiting on you, teal = live - used identically for an ads
approval, incomplete Stripe onboarding and a DNS-pending domain. `<Status>` is
that primitive, and its type makes a fourth state unrepresentable on purpose.

`<CreditGauge>` is the one element here allowed visual presence, because SPEC
§18 asks for it by name: build and runtime as separate bars with the active hold
drawn as hatching, on every screen, because "users need to see burn while
causing it". There is no credits endpoint until phase 4, so it renders an
explicit unknown state rather than zero - zero would read as "you are out of
credits".

Components reference semantic tokens only. A component that reaches a `--raw-*`
value keeps its colour when the mockup remaps the semantic one, so the swap
would miscolour silently instead of failing.
