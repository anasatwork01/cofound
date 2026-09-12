import { Suspense } from "react"
import type { Metadata } from "next"
import { PageHeader } from "@/components/page-header"
import { AuthCallback } from "./auth-callback"

export const metadata: Metadata = { title: "Sign in" }

/**
 * SPEC §18: "an action keeps its name through the whole flow", so the heading
 * is still "Sign in" — this is the end of the thing that started there, not a
 * new screen with a new name.
 *
 * The `<Suspense>` boundary is required rather than decorative: `useSearchParams`
 * makes everything below it client-rendered, and Next refuses to prerender a
 * client hook that reads URL data outside one.
 */
export default function AuthCallbackPage() {
  return (
    <div className="mx-auto w-full max-w-md space-y-8 px-5 py-16">
      <PageHeader title="Sign in" />
      <Suspense
        fallback={
          <section className="rounded-md border border-border bg-surface-raised p-6">
            <h2 className="text-xl font-semibold text-ink">Signing you in</h2>
            <p className="mt-3 text-base text-ink-muted">This takes a moment.</p>
          </section>
        }
      >
        <AuthCallback />
      </Suspense>
    </div>
  )
}
