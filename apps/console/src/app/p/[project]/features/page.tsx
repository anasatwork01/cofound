import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Features" }

/**
 * The capability catalogue: sign-in, payments, email, uploads, Slack. The
 * phase 5 tasks fill it — 5.1 for the manifest and catalogue, 5.2 for the
 * installer, and one task per capability after that.
 */
export default function FeaturesPage() {
  return (
    <>
      <PageHeader
        title="Features"
        lede="Sign-in, payments, email, file uploads. Added properly, with their keys kept where the sandbox cannot read them."
      />
      <EmptyState
        title="No features added yet"
        body="Ask for the one you need in the builder, in the words you would use out loud, and it arrives wired up — with anything that needs your approval brought to you here."
      />
    </>
  )
}
