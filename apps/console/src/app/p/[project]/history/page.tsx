import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "History" }

/**
 * The timeline and restore. Task 2.12 fills it, and keeps "roll back the live
 * site" and "restore this version" as two distinct actions.
 *
 * SPEC §18's copy rule bites hardest on this screen: "You edited 2 files", not
 * "commit c02b7ad". The SHA belongs here — a support conversation needs it —
 * but small, monospace, and never as the name of a version.
 */
export default function HistoryPage() {
  return (
    <>
      <PageHeader title="History" lede="Every change, in order, and a way back to any of them." />
      <EmptyState
        title="No changes yet"
        body="Each thing you ask for is kept as a version you can read, compare and restore. The first one appears as soon as the agent finishes."
      />
    </>
  )
}
