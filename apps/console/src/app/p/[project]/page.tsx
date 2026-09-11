import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { LeaseBanner } from "@/components/lease-banner"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Builder" }

/**
 * The builder: chat on one side, preview on the other.
 *
 * Task 1.16 fills it — streaming turn cards, tool events appended to the
 * current card, unknown event types ignored, the preview pane — on top of the
 * SSE gateway from task 1.14. The write-lease banner SPEC §18 requires is
 * already here and already wired to the store, because the rule it enforces
 * ("never a silently rejecting editor") has to be true from the first turn.
 */
export default function BuilderPage() {
  return (
    <>
      <PageHeader
        title="Builder"
        lede="Ask for what you want in your own words. The agent writes the code and you watch it happen."
      />
      <LeaseBanner />
      <EmptyState
        title="Nothing built yet"
        body="Describe the first thing this project should do — a page, a form, a whole idea — and the agent starts on it straight away."
      />
    </>
  )
}
