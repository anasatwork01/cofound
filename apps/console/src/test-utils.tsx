import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, type RenderResult } from "@testing-library/react"
import { vi } from "vitest"
import type { ReactElement, ReactNode } from "react"

/**
 * Render a client component that queries.
 *
 * Retries are off and there is no cache between tests: a test that asserts an
 * error state should not have to wait out the production retry policy, and a
 * cache shared between tests is a test that passes because of the one before
 * it. This mirrors what `providers.tsx` does per render tree, for the same
 * reason — one client per mount, never a shared one.
 */
export function renderWithQuery(
  ui: ReactElement,
  /**
   * Override the client for a test that has to read the cache back.
   *
   * The default sets `gcTime: 0`, which collects a query the instant nothing is
   * observing it — so a test asserting that something was written to the cache
   * reads `undefined` and looks like a bug in the component. Pass a client with
   * a real `gcTime` when the cache itself is what is under test.
   */
  client?: QueryClient,
): RenderResult & { queryClient: QueryClient } {
  const queryClient =
    client ??
    new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
    })
  const result = render(ui, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
  return Object.assign(result, { queryClient })
}

/**
 * Re-exported so a `.tsx` test has one import for everything it needs. The
 * definitions live in `test-api.ts`, which carries no JSX — see the note there:
 * the node-side test project cannot parse this file at all.
 */
export { envelope, sessionBody, stubApi } from "./test-api"
export type { StubbedCall, StubbedResponse } from "./test-api"

/**
 * Every text node in the document, joined with spaces.
 *
 * NOT `document.body.textContent`, and the difference is load-bearing:
 * `textContent` concatenates with no separator, so a heading runs straight into
 * the paragraph below it — "Check your email" + "We found your account" becomes
 * "Check your emailWe found your account". A pattern anchored with `\b` then
 * finds no word boundary at that seam and silently fails to match, which turns
 * a "this copy must never appear" assertion into one that passes whatever the
 * copy says. Measured: exactly that made the anti-enumeration guard in
 * `sign-in-form.test.tsx` pass against "We found your account."
 */
export function readableText(): string {
  const nodes = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
  const parts: string[] = []
  for (let node = nodes.nextNode(); node !== null; node = nodes.nextNode()) {
    const text = (node.textContent ?? "").trim()
    if (text !== "") parts.push(text)
  }
  return parts.join(" ")
}
