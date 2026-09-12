"use client"

import { useQuery } from "@tanstack/react-query"
import { ErrorNotice } from "@/components/error-notice"
import { fetchTemplates, queryKeys, type Template } from "@/lib/api"

/**
 * The template gallery from SPEC §18, reading the real `GET /templates`.
 *
 * Every shape comes from the generated `api-v1` types, so if the endpoint's
 * response changes in `packages/schema` this stops compiling rather than
 * quietly rendering nothing.
 *
 * SEAM: choosing a template still does not create a project, and the reason is
 * no longer a missing endpoint — task 0.14 wired `POST /projects` for the
 * prompt box next door. It is the name. §7.1's template variant REQUIRES one
 * (`required: [org_id, name, template_version_id]`), the API derives the
 * project slug from it, and a slug is unique per org — so a second project
 * started from the same template with the template's own name is refused with
 * "That project name is already taken in this organisation." A template card
 * therefore needs a name field before it can become a button, and inventing
 * "Storefront 2" on the reader's behalf is not that. Until then a card is a
 * description: one that looked clickable and was not would be worse than one
 * that never claimed to be.
 */
export function TemplateGallery() {
  const templates = useQuery({ queryKey: queryKeys.templates, queryFn: fetchTemplates })

  if (templates.isPending) {
    return (
      <section aria-busy="true" className="space-y-4">
        <h2 className="text-lg font-semibold text-ink">Templates</h2>
        <p className="sr-only">Loading templates</p>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((index) => (
            <div key={index} className="h-28 rounded-md border border-border bg-surface-sunken" />
          ))}
        </div>
      </section>
    )
  }

  if (templates.isError) {
    return (
      <section className="space-y-4">
        <h2 className="text-lg font-semibold text-ink">Templates</h2>
        {/*
          The API's own words, both halves of them.

          This branch used to print "The templates did not load." over a
          hard-coded "Check your connection, then try again." — so a clean 401,
          whose entire content is "You are not signed in." / "Sign in and try
          again.", read as a network fault and sent the reader to look at their
          wifi. `common.schema.json` splits `message` and `fix` precisely so
          that cannot happen; `ErrorNotice` surfaces both, and owns a sentence
          only when there was no response at all to surface.
        */}
        <ErrorNotice
          error={templates.error}
          action={{
            label: "Try again",
            onClick: () => {
              void templates.refetch()
            },
          }}
        />
      </section>
    )
  }

  const items = [...templates.data.templates].sort(readyFirst)

  if (items.length === 0) {
    return (
      <section className="space-y-4">
        <h2 className="text-lg font-semibold text-ink">Templates</h2>
        <p className="max-w-prose rounded-md border border-border bg-surface-raised p-5 text-base text-ink-muted">
          No templates are installed yet. Describe what you want in the box above instead — the
          agent will start from an empty project and build it from there.
        </p>
      </section>
    )
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold text-ink">Templates</h2>
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((template) => (
          <li key={template.id} className="rounded-md border border-border bg-surface-raised p-5">
            <h3 className="text-base font-medium text-ink">{template.display_name}</h3>
            {template.summary === undefined ? null : (
              <p className="mt-2 text-sm text-ink-muted">{template.summary}</p>
            )}
            <p className="mt-3 font-mono text-xs text-ink-muted">
              {template.current_version.version}
            </p>
            {template.current_version.image_ready ? null : (
              <p className="mt-2 text-xs text-ink-muted">
                Takes a little longer to start just now.
              </p>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

/**
 * A template whose sandbox image is baked starts in seconds; one without it
 * waits for a build (SPEC §9's p50 under 10s budget). Showing those second is
 * the whole of the UI's response to it — no badge, because "image" is not a
 * word this audience should have to learn.
 */
function readyFirst(a: Template, b: Template): number {
  const ready = Number(b.current_version.image_ready) - Number(a.current_version.image_ready)
  return ready !== 0 ? ready : a.display_name.localeCompare(b.display_name)
}
