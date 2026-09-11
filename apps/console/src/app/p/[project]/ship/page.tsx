import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Ship" }

/**
 * Versions, domains, repo and database. Task 3.11 fills it, on top of the
 * publish pipeline in 3.5.
 *
 * The button on this screen will say "Publish" and the toast it produces will
 * say "Published" — SPEC §18, an action keeps its name through the whole flow.
 */
export default function ShipPage() {
  return (
    <>
      <PageHeader
        title="Ship"
        lede="Put it online, point a domain at it, and see what is running where."
      />
      <EmptyState
        title="Not published yet"
        body="Publish when it is ready and this project gets an address you can send to anyone. You can keep building afterwards; nothing goes live again until you say so."
      />
    </>
  )
}
