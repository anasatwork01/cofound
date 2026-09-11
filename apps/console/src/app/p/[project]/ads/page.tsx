import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { routes } from "@/lib/routes"

export const metadata: Metadata = { title: "Ads" }

/**
 * Approvals, KPIs, campaigns. Task 6.7 fills it, on top of 6.5.
 *
 * The approval queue here is SPEC §18's first example of amber: waiting on
 * you, drawn exactly the way incomplete Stripe onboarding and a DNS-pending
 * domain are drawn. One learned pattern, not three.
 */
export default function AdsPage() {
  return (
    <>
      <PageHeader
        title="Ads"
        lede="Campaigns the agent drafts, budgets you approve, and what came back."
      />
      <EmptyState
        title="No campaigns yet"
        body="Connect an ad account and the agent can draft campaigns against what your site actually says. Nothing spends until you approve it."
        action={{ href: routes.connections, label: "Connect an ad account" }}
      />
    </>
  )
}
