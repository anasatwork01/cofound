import type { Metadata } from "next"
import { HomeRouter } from "./home-router"

/* `absolute`, because the root layout's template would otherwise render this
   as "Halyard · Halyard". */
export const metadata: Metadata = { title: { absolute: "Halyard" } }

/**
 * SPEC §18: "`/` → redirect to last project or /new".
 *
 * It used to be a bare server-side `redirect()` to `/new`, which is the right
 * destination for exactly one of the three people who can arrive here and the
 * wrong one for the other two: a visitor who has never signed in, and a
 * signed-in user who is in no organisation yet, both landed on a screen that
 * cannot do anything for them.
 *
 * The decision cannot be made on the server. SPEC §8 puts the session in an
 * httpOnly cookie on the API's origin, and this page is served from another —
 * so the only thing that can read `GET /auth/session` is the browser, and the
 * routing has to happen after it answers. `HomeRouter` does that and holds
 * still until it has, which is why `/` renders something rather than nothing.
 *
 * Still a Server Component: it has no state of its own, and pages in this app
 * are server components by default (`tests/console/shell-structure.test.ts`
 * enforces it).
 */
export default function HomePage() {
  return <HomeRouter />
}
