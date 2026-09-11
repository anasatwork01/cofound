import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { LeaseBanner } from "@/components/lease-banner"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Files" }

/**
 * Tree on the left, editor on the right. Task 2.7 fills it: the file tree, a
 * CodeMirror 6 editor (SPEC §3.1 — not Monaco), and the size cap and signed
 * download that binary and large files need.
 *
 * The lease banner is here for the reason SPEC §18 gives it: this is the
 * screen with the editor on it, and an editor that rejects keystrokes without
 * saying why is the exact failure the rule exists to prevent.
 */
export default function FilesPage() {
  return (
    <>
      <PageHeader
        title="Files"
        lede="Everything the agent wrote, open to read and open to change."
      />
      <LeaseBanner />
      <EmptyState
        title="Nothing to open yet"
        body="Every file the agent writes shows up here the moment it does. Ask for the first thing in the builder and come back."
      />
    </>
  )
}
