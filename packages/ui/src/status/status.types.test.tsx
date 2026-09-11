import { expect, it } from "vitest"
import { Status } from "./status"

/**
 * A type-level test. `tsc --noEmit` is what runs it — `pnpm typecheck` fails if
 * any of these `@ts-expect-error` comments stops being true, which is the only
 * way to assert that a fourth state is unrepresentable. Vitest strips the types
 * and just checks the elements exist.
 *
 * SPEC §18 fixes three states and says they are "one learned pattern, not
 * three". The moment a caller can pass `state="error"`, or restyle a badge into
 * a colour the user has not learned, the pattern is four states and unlearnable.
 */
it("admits exactly three states and offers no way round them", () => {
  const elements = [
    // @ts-expect-error - there is no fourth state. SPEC §18 allows three.
    <Status key="fourth" state="error" />,
    // @ts-expect-error - no className: a caller cannot repaint a state, not
    // even with another state's token.
    <Status key="repainted" state="live" className="bg-agent" />,
    // @ts-expect-error - no style, for the same reason.
    <Status key="styled" state="live" style={{ opacity: 0.5 }} />,
    // @ts-expect-error - the state is required; an unstyled badge means nothing.
    <Status key="stateless">Published</Status>,
  ]
  expect(elements).toHaveLength(4)
})
