import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Connections" }

/**
 * GitHub, Google, Meta, Slack. The phase 5 tasks fill it: 5.15 for the GitHub
 * App install flow, 5.14 for Slack, and the ads and search work in phase 6 for
 * the Google and Meta accounts.
 *
 * Every one of these is an OAuth grant held by the control plane, never by the
 * sandbox (SPEC §17.1). Half-finished ones are amber here — waiting on you —
 * exactly as they are everywhere else.
 */
export default function ConnectionsPage() {
  return (
    <>
      <PageHeader
        title="Connections"
        lede="The accounts this organisation has connected, and what each one lets the agent do."
      />
      <EmptyState
        title="Nothing connected yet"
        body="Connect GitHub to keep a copy of the code in your own account, or Google, Meta and Slack to let the agent work where you already do. You can disconnect any of them later."
      />
    </>
  )
}
