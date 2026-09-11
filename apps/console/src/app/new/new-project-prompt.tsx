"use client"

import { useId, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { fetchSession, queryKeys } from "@/lib/api"

/**
 * The prompt box from SPEC §18.
 *
 * SEAM: nothing here creates a project yet. `POST /projects` is specified in
 * `api.openapi.yaml` and takes an org and a prompt, but the service behind it
 * is task 0.9 and the builder it would hand off to is task 1.16 — and the
 * organisation to create it in comes from the project picker in the top bar,
 * which has nowhere to put a current org yet. So the action is present, named,
 * and marked unavailable with the reason attached to it: SPEC §18 has no room
 * for a control that silently does nothing.
 *
 * Whoever wires it: keep the button's name. "Start building" here has to
 * become "Starting" and then land you in the builder, not turn into "Create".
 *
 * ---------------------------------------------------------------------------
 * WHY `aria-disabled` AND NOT `disabled`
 *
 * A `disabled` button is not focusable, so the `aria-describedby` reason on it
 * is attached to the one element on the screen that can never deliver it: a
 * keyboard or screen-reader user could not reach the button to hear why it
 * cannot act. `aria-disabled` keeps it in the tab order, announces it as
 * unavailable, and leaves the reason reachable; the form's submit handler is
 * what makes it inert.
 *
 * It is also not dimmed. `disabled:opacity-60` composited the violet fill and
 * its white text down to 2.80:1 — WCAG exempts a genuinely inactive control,
 * but this button is focusable now, and "unavailable until 0.9 lands" is the
 * only state it has, so dimming it would mean shipping a screen whose primary
 * action nobody can read. At full strength the fill carries its text at
 * 6.80:1 and the sentence beside it, not a wash of grey, says why it will not
 * go yet.
 */
export function NewProjectPrompt() {
  const [prompt, setPrompt] = useState("")
  const promptId = useId()
  const reasonId = useId()

  // A real endpoint, and the only one that can answer "can this person start a
  // project at all". It is also how the shell proves the query layer is live.
  const session = useQuery({ queryKey: queryKeys.session, queryFn: fetchSession })

  const reason = session.isPending
    ? "Checking who you are."
    : session.isSuccess
      ? "The builder is not connected yet, so projects cannot start from here."
      : "Sign in to start a project."

  return (
    <form
      className="space-y-3"
      onSubmit={(event) => {
        // The whole of the action, until there is something to submit to. The
        // button is reachable on purpose (see above), so a click and an Enter
        // both arrive here, and both stop here.
        event.preventDefault()
      }}
    >
      <label htmlFor={promptId} className="block text-sm font-medium text-ink">
        What do you want to build?
      </label>
      {/* The border is the only thing on the screen saying "this is a box you
          type in" — no fill contrast to fall back on, since the field and the
          page are both near-white — so it is the boundary token that clears
          WCAG 2.2 SC 1.4.11's 3:1 rather than the decorative one, which was
          1.45:1 against the field and 1.09:1 of field against page. */}
      <textarea
        id={promptId}
        name="prompt"
        rows={4}
        value={prompt}
        onChange={(event) => setPrompt(event.target.value)}
        placeholder="A booking page for my studio, with times I set and a deposit taken up front."
        className="block w-full rounded-md border border-border-strong bg-surface-raised p-3 text-sm text-ink placeholder:text-ink-muted"
      />
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="submit"
          aria-disabled="true"
          aria-describedby={reasonId}
          className="cursor-not-allowed rounded-md bg-agent px-4 py-2 text-sm font-medium text-agent-ink"
        >
          Start building
        </button>
        <p id={reasonId} className="text-sm text-ink-muted">
          {reason}
        </p>
      </div>
    </form>
  )
}
