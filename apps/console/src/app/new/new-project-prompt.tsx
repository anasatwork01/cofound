"use client"

import { useId, useState } from "react"
import { useRouter } from "next/navigation"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { CreateProjectFromPromptSchema } from "@halyard/schema/zod/api-v1"
import { ErrorNotice } from "@/components/error-notice"
import { createProject, queryKeys, type CreateProjectRequest, type Project } from "@/lib/api"
import { useCurrentOrg } from "@/lib/current-org"
import { routes } from "@/lib/routes"
import { useSession } from "@/lib/session"

/**
 * The prompt box from SPEC §18, wired to `POST /v1/projects`.
 *
 * §7.1 takes either a template version or a prompt, never both; with a prompt
 * the agent picks the template itself, which is why this sends no template and
 * no name. The API names a prompt-started project for you until task 1.x lets
 * the agent do it.
 *
 * The request carries the org TWICE, and both are load-bearing: `X-Halyard-Org`
 * selects the tenancy scope the request runs in, and `org_id` in the body says
 * which org the caller believes they are creating in. The API refuses when they
 * disagree rather than honouring the body — a request cannot create a project
 * in an org whose role was never checked.
 *
 * ---------------------------------------------------------------------------
 * WHY `aria-disabled` AND NOT `disabled`, WHEN IT CANNOT ACT
 *
 * A `disabled` button is not focusable, so the `aria-describedby` reason on it
 * is attached to the one element on the screen that can never deliver it: a
 * keyboard or screen-reader user could not reach the button to hear why it
 * cannot act. `aria-disabled` keeps it in the tab order, announces it as
 * unavailable, and leaves the reason reachable; the submit handler is what
 * makes it inert.
 *
 * It is also not dimmed. `disabled:opacity-60` composited the violet fill and
 * its white text down to 2.80:1 — WCAG exempts a genuinely inactive control,
 * but this button is focusable, so dimming it would mean the primary action of
 * the entry screen is the one thing on it nobody can read. At full strength the
 * fill carries its text well clear of 4.5:1, and the sentence beside it says
 * why it will not go yet.
 */
export function NewProjectPrompt() {
  const [prompt, setPrompt] = useState("")
  const promptId = useId()
  const reasonId = useId()

  const router = useRouter()
  const queryClient = useQueryClient()

  const state = useSession()
  const orgs = state.status === "signed-in" ? state.session.orgs : []
  const { current } = useCurrentOrg(orgs)

  const create = useMutation({
    mutationFn: (variables: { readonly body: CreateProjectRequest; readonly org: string }) =>
      createProject(variables.body, variables.org),
    onSuccess: async (project: Project, variables) => {
      // The list on this very screen is now wrong. Awaited so the builder is
      // pushed onto a cache that already knows the project exists — otherwise
      // a Back from the builder shows a list without it in.
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects(variables.org) })
      router.push(routes.builder(project.slug))
    },
  })

  // One place that decides whether the action can act, and the sentence that
  // says why not. Deriving both from the same expression is what stops the
  // button and its reason from disagreeing.
  const blocked = whyBlocked({ state, hasOrg: current !== null, prompt })

  return (
    <div className="space-y-4">
      <form
        noValidate
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          if (blocked !== null || create.isPending || current === null) return

          const parsed = CreateProjectFromPromptSchema.safeParse({
            org_id: current.id,
            prompt: prompt.trim(),
          })
          // Unreachable while `blocked` is null — the guard above already
          // required a non-empty prompt and an org — so this is the contract
          // catching a shape the console got wrong, not a user error to render.
          if (!parsed.success) return

          create.mutate({
            body: { org_id: parsed.data.org_id, prompt: parsed.data.prompt },
            org: current.slug,
          })
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
          className="block w-full rounded-sm border border-border-strong bg-surface-raised p-4 text-base text-ink placeholder:text-ink-muted"
        />
        <div className="flex flex-wrap items-center gap-4">
          <button
            type="submit"
            aria-disabled={blocked === null ? undefined : true}
            aria-describedby={blocked === null ? undefined : reasonId}
            className={`rounded-sm bg-agent px-5 py-2.5 text-base font-medium text-agent-ink ${
              blocked === null ? "" : "cursor-not-allowed"
            }`}
          >
            {/* SPEC §18: an action keeps its name through the whole flow. */}
            {create.isPending ? "Starting" : "Start building"}
          </button>
          {blocked === null ? null : (
            <p id={reasonId} className="text-sm text-ink-muted">
              {blocked}
            </p>
          )}
        </div>
      </form>

      {create.isError ? <ErrorNotice error={create.error} /> : null}
    </div>
  )
}

/**
 * The sentence explaining why "Start building" will not go, or `null` when it
 * will.
 *
 * Exported so the rule can be read and tested as a rule. Every branch names
 * something the reader can do next, which is the half of SPEC §18's error
 * contract that applies to a control that is merely not ready yet.
 */
export function whyBlocked(input: {
  readonly state: { readonly status: string }
  readonly hasOrg: boolean
  readonly prompt: string
}): string | null {
  if (input.state.status === "loading") return "Checking who you are."
  if (input.state.status === "signed-out") return "Sign in to start a project."
  if (input.state.status === "unreachable") {
    return "Halyard could not be reached. Check your connection, then reload."
  }
  if (!input.hasOrg) return "Create an organisation first — projects belong to one."
  if (input.prompt.trim() === "") return "Describe what you want built, then start."
  return null
}
