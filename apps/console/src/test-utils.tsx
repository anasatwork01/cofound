import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, type RenderResult } from "@testing-library/react"
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
export function renderWithQuery(ui: ReactElement): RenderResult & { queryClient: QueryClient } {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  const result = render(ui, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
  return Object.assign(result, { queryClient })
}
