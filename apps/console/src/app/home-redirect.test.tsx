import { describe, expect, it } from "vitest"
import HomePage from "@/app/page"

/**
 * SPEC §18: "/ → redirect to last project or /new".
 *
 * `redirect()` works by throwing, and the digest it throws carries the target,
 * so this asserts on the destination rather than on the fact of a throw — a
 * redirect to the wrong screen would otherwise pass.
 */
describe("/", () => {
  it("sends you to /new until there is a last project to send you to", () => {
    let digest = "no redirect"
    try {
      HomePage()
    } catch (error) {
      digest = String((error as { digest?: unknown }).digest)
    }
    expect(digest).toMatch(/^NEXT_REDIRECT;[a-z]+;\/new;/)
  })
})
