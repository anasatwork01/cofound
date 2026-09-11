import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Search" }

/**
 * SEO issues, positions, site health. Task 6.14 fills it, on top of 6.13.
 */
export default function SearchPage() {
  return (
    <>
      <PageHeader
        title="Search"
        lede="What people could find, what is stopping them, and where you rank for it."
      />
      <EmptyState
        title="Nothing to check yet"
        body="Publish the project and this fills with the issues worth fixing, in the order worth fixing them, and the searches you are showing up for."
      />
    </>
  )
}
