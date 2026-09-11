import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { routes } from "@/lib/routes"

export const metadata: Metadata = { title: "Not found" }

/**
 * SPEC §18: errors say what happened and how to fix it, and never apologise.
 * "Page not found" says what happened; the link says what to do instead.
 */
export default function NotFound() {
  return (
    <div className="mx-auto w-full max-w-4xl space-y-6 px-5 py-8">
      <PageHeader
        title="That page is not here"
        lede="The address may have changed, or the project may have been renamed."
      />
      <EmptyState
        title="Nothing at this address"
        body="Check the address if you typed it, or start somewhere you know exists."
        action={{ href: routes.newProject, label: "Start a project" }}
      />
    </div>
  )
}
