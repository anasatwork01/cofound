import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Credits" }

/**
 * Balance, ledger, the build/runtime split and auto top-up. Task 4.9 fills it,
 * on top of the metering in 4.5.
 *
 * Nothing on this screen fetches anything: `api.openapi.yaml` puts credits and
 * the ledger in phase 4 and specifies no endpoint for either yet, and writing
 * a client against an endpoint nobody has specified is what CLAUDE.md working
 * agreement 4 forbids. The top bar's gauge says "unknown" for the same reason.
 */
export default function CreditsPage() {
  return (
    <>
      <PageHeader
        title="Credits"
        lede="What you have, what building spent and what running the site is spending."
      />
      <EmptyState
        title="No charges yet"
        body="Start building and every charge appears here, line by line. Building and running are counted separately, so a busy site never eats the credits you were going to build with."
      />
    </>
  )
}
