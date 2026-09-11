"use client"

import { useQuery } from "@tanstack/react-query"
import { ApiError, fetchTemplates, queryKeys, type Template } from "@/lib/api"

/**
 * The template gallery from SPEC §18, reading the real `GET /templates`.
 *
 * Every shape comes from the generated `api-v1` types, so if the endpoint's
 * response changes in `packages/schema` this stops compiling rather than
 * quietly rendering nothing.
 *
 * SEAM: choosing a template creates a project, which is the same missing
 * `POST /projects` the prompt box is waiting on. Until then a card is a
 * description, not a button — a card that looked clickable and was not would
 * be worse than one that never claimed to be.
 */
export function TemplateGallery() {
  const templates = useQuery({ queryKey: queryKeys.templates, queryFn: fetchTemplates })

  if (templates.isPending) {
    return (
      <section aria-busy="true" className="space-y-3">
        <h2 className="text-base font-medium text-ink">Templates</h2>
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
    const error = templates.error
    const said = error instanceof ApiError ? error : null
    return (
      <section className="space-y-3">
        <h2 className="text-base font-medium text-ink">Templates</h2>
        {/*
          DELIBERATE: a failure is drawn in neutral surface tokens, not in
          amber.

          SPEC §18 fixes amber as "waiting on you" and requires it to mean the
          same thing every time: an ads approval, an incomplete Stripe
          onboarding, a DNS-pending domain. Those are states the product is
          correctly in, holding still for a decision only the reader can make.
          A request that failed is none of them — nothing is waiting on a
          decision, a read broke and a retry may well fix it without the reader
          deciding anything. Painting it amber would quietly redefine the state
          to "waiting on you, or broken", and then the three states stop being
          one learned pattern.

          §18 defines no error state, and this is the first place that gap gets
          filled, so it is filled with nothing: `role="alert"` carries the
          urgency, the copy carries the meaning — what happened, then how to fix
          it — and the boundary is the token that has to be seen, so the notice
          is still findable without borrowing a colour that means something
          else. If a later screen needs error to be a COLOUR, that is a change
          to §18's vocabulary and belongs in the spec, not in a component.
        */}
        <div
          role="alert"
          className="max-w-prose rounded-lg border border-border-strong bg-surface-raised p-4"
        >
          <p className="text-sm font-medium text-ink">The templates did not load.</p>
          <p className="mt-2 text-sm text-ink">
            {said?.fix ?? "Check your connection, then try again."}
          </p>
          <button
            type="button"
            onClick={() => {
              void templates.refetch()
            }}
            className="mt-3 rounded-md border border-border-strong bg-surface-raised px-3 py-2 text-sm font-medium text-ink"
          >
            Try again
          </button>
          {said?.requestId === undefined ? null : (
            <p className="mt-3 font-mono text-xs text-ink-muted">{said.requestId}</p>
          )}
        </div>
      </section>
    )
  }

  const items = [...templates.data.templates].sort(readyFirst)

  if (items.length === 0) {
    return (
      <section className="space-y-3">
        <h2 className="text-base font-medium text-ink">Templates</h2>
        <p className="max-w-prose rounded-lg border border-border bg-surface-raised p-4 text-sm text-ink-muted">
          No templates are installed yet. Describe what you want in the box above instead — the
          agent will start from an empty project and build it from there.
        </p>
      </section>
    )
  }

  return (
    <section className="space-y-3">
      <h2 className="text-base font-medium text-ink">Templates</h2>
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((template) => (
          <li key={template.id} className="rounded-md border border-border bg-surface-raised p-4">
            <h3 className="text-sm font-medium text-ink">{template.display_name}</h3>
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
