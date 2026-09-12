import type { Metadata } from "next"
import { PageHeader } from "@/components/page-header"
import { NewProjectPrompt } from "./new-project-prompt"
import { ProjectList } from "./project-list"
import { TemplateGallery } from "./template-gallery"

export const metadata: Metadata = { title: "New project" }

/**
 * SPEC §18: "/new → template gallery + prompt box".
 *
 * Plus, since task 0.14, the project list — which is not a third thing but the
 * other half of the same one. `/` sends every signed-in reader here, so this is
 * the console's home screen, and a home screen that shows what you could start
 * and not what you already started would send you looking for a second one. The
 * order is the reading order: start something, see what you started, see what
 * you could start from.
 *
 * A server component holding three client ones: each of them has state or
 * reads the API, and the page itself is static structure with no reason to ship
 * to the browser.
 */
export default function NewProjectPage() {
  return (
    <div className="mx-auto w-full max-w-5xl space-y-10 px-5 py-10">
      <PageHeader
        title="Start something"
        lede="Describe what you want to exist. Or start from a template and change it from there."
      />
      <NewProjectPrompt />
      <ProjectList />
      <TemplateGallery />
    </div>
  )
}
