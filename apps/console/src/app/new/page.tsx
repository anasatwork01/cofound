import type { Metadata } from "next"
import { PageHeader } from "@/components/page-header"
import { NewProjectPrompt } from "./new-project-prompt"
import { TemplateGallery } from "./template-gallery"

export const metadata: Metadata = { title: "New project" }

/**
 * SPEC §18: "/new → template gallery + prompt box".
 *
 * A server component holding two client ones: the prompt box has local state
 * and the gallery reads `GET /templates` through TanStack Query, but the page
 * itself is static structure and has no reason to ship to the browser.
 */
export default function NewProjectPage() {
  return (
    <div className="mx-auto w-full max-w-5xl space-y-8 px-5 py-8">
      <PageHeader
        title="Start something"
        lede="Describe what you want to exist. Or start from a template and change it from there."
      />
      <NewProjectPrompt />
      <TemplateGallery />
    </div>
  )
}
