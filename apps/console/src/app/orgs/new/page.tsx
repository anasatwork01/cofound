import type { Metadata } from "next"
import { PageHeader } from "@/components/page-header"
import { CreateOrgForm } from "./create-org-form"

export const metadata: Metadata = { title: "New organisation" }

/**
 * Creating an organisation — the middle step of phase 0's acceptance criterion,
 * and the one with nowhere in SPEC §18's route table to live.
 *
 * It cannot go under `/settings/*`: those three screens are scoped to an
 * organisation you are already in, and this is what you do when you are in
 * none. `@/lib/routes` carries the full argument.
 */
export default function NewOrgPage() {
  return (
    <div className="mx-auto w-full max-w-md space-y-8 px-5 py-16">
      <PageHeader
        title="Create an organisation"
        lede="Projects, credits and teammates all belong to an organisation. Yours can be just you — invite people whenever you want."
      />
      <CreateOrgForm />
    </div>
  )
}
