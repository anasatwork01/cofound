"use client"

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { shouldRetry } from "@/lib/api"

/**
 * Everything the client tree needs, mounted once in the root layout.
 *
 * The QueryClient is created inside `useState` rather than at module scope.
 * This is the one thing the TanStack SSR guide insists on and the classic bug
 * if you skip it: a module-scope client is shared by every request the server
 * handles, so one user's cached `GET /auth/session` is served to the next user
 * who arrives. `useState(() => ...)` gives each render tree its own, and the
 * initialiser runs once per mount rather than on every render.
 *
 * No devtools: `@tanstack/react-query-devtools` is not a dependency of this
 * repository, and adding one is not this task's to make.
 */
export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // Long enough that moving between screens does not refetch the
            // session and the project list on every navigation; short enough
            // that a stale org membership corrects itself without a reload.
            staleTime: 30_000,
            gcTime: 5 * 60_000,
            retry: shouldRetry,
            // A dashboard left open in a background tab should not stampede
            // the API the moment its owner comes back to it.
            refetchOnWindowFocus: false,
          },
          // A retried mutation is a second charge, a second project, a second
          // invite. SPEC §7.1 gives every mutating endpoint an
          // `Idempotency-Key` for when a retry is genuinely wanted; until a
          // call sends one, it is not retried.
          mutations: { retry: false },
        },
      }),
  )

  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}
