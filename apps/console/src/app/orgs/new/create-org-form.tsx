"use client"

import { useId, useState } from "react"
import { useRouter } from "next/navigation"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { CreateOrgRequestSchema } from "@halyard/schema/zod/api-v1"
import { ErrorNotice } from "@/components/error-notice"
import { createOrg, queryKeys, type CreateOrgRequest } from "@/lib/api"
import { rememberOrg } from "@/lib/current-org"
import { routes } from "@/lib/routes"

/**
 * `POST /v1/orgs`.
 *
 * ---------------------------------------------------------------------------
 * WHY THERE IS A SECOND FIELD
 *
 * The API derives a slug from the name when you send none, and answers a name
 * collision with "That organisation name is already taken." / "Pick a different
 * name, or set a slug explicitly." SPEC §18 requires a fix the reader can act
 * on, and half of that one is unreachable from a form with no slug field — so
 * the field exists, optional, and labelled in the reader's words rather than in
 * the schema's. Two people at the same company can then both be "Acme" without
 * one of them having to rename their organisation.
 *
 * ---------------------------------------------------------------------------
 * NOT GATED ON THE SESSION, DELIBERATELY
 *
 * The form renders straight away rather than waiting to hear whether you are
 * signed in. The only authority on whether you may create an org is the API,
 * which checks it on the request; a client-side guess would mean a spinner in
 * front of a form for everyone, and a wrong guess flashing the wrong screen.
 * A signed-out reader gets the API's own 401 — "You are not signed in." / "Sign
 * in and try again." — with the link that fix names, which is a better answer
 * than a redirect that loses what they typed.
 */
export function CreateOrgForm() {
  const [name, setName] = useState("")
  const [shortName, setShortName] = useState("")
  const [invalid, setInvalid] = useState<{ field: "name" | "slug"; message: string } | null>(null)
  const nameId = useId()
  const shortNameId = useId()
  const shortNameHintId = useId()
  const invalidId = useId()

  const router = useRouter()
  const queryClient = useQueryClient()

  const create = useMutation({
    mutationFn: (body: CreateOrgRequest) => createOrg(body),
    onSuccess: async (org) => {
      // Creating an org makes the caller its owner in the same transaction, so
      // the session's `orgs` list is stale the instant this resolves — and
      // `orgs` is what every screen reads to decide which org it is acting as.
      // Awaited, so the next screen renders against the new list rather than
      // against the one that has no orgs in it.
      rememberOrg(org.slug)
      await queryClient.invalidateQueries({ queryKey: queryKeys.session })
      router.push(routes.newProject)
    },
  })

  return (
    <div className="space-y-6">
      <form
        noValidate
        className="space-y-5"
        onSubmit={(event) => {
          event.preventDefault()
          if (create.isPending) return

          const trimmedName = name.trim()
          const trimmedShortName = shortName.trim()
          const parsed = CreateOrgRequestSchema.safeParse(
            trimmedShortName === ""
              ? { name: trimmedName }
              : { name: trimmedName, slug: trimmedShortName },
          )
          if (!parsed.success) {
            setInvalid(
              trimmedName === "" || trimmedName.length > 200
                ? {
                    field: "name",
                    message:
                      trimmedName === ""
                        ? "Give the organisation a name. It is what everyone in it will see."
                        : "That name is too long. Keep it under 200 characters.",
                  }
                : {
                    field: "slug",
                    message:
                      "A short name can use lowercase letters, numbers and hyphens, and has to start and end with a letter or number.",
                  },
            )
            return
          }
          setInvalid(null)
          // Rebuilt rather than passed through. zod types an optional property
          // as `T | undefined`; under this repo's `exactOptionalPropertyTypes`
          // that is a different type from `T | absent`, and the API's
          // `CreateOrgRequest` means the second — a `slug` present and
          // undefined is not the same request as no `slug` at all.
          create.mutate(
            parsed.data.slug === undefined
              ? { name: parsed.data.name }
              : { name: parsed.data.name, slug: parsed.data.slug },
          )
        }}
      >
        <div className="space-y-2">
          <label htmlFor={nameId} className="block text-sm font-medium text-ink">
            Organisation name
          </label>
          <input
            id={nameId}
            name="name"
            type="text"
            autoComplete="organization"
            value={name}
            onChange={(event) => {
              setName(event.target.value)
              if (invalid !== null) setInvalid(null)
            }}
            aria-invalid={invalid?.field === "name" ? true : undefined}
            aria-describedby={invalid?.field === "name" ? invalidId : undefined}
            className="block w-full rounded-sm border border-border-strong bg-surface-raised px-3 py-2.5 text-base text-ink"
          />
          {invalid?.field === "name" ? (
            <p id={invalidId} className="text-sm text-ink">
              {invalid.message}
            </p>
          ) : null}
        </div>

        <div className="space-y-2">
          <label htmlFor={shortNameId} className="block text-sm font-medium text-ink">
            Short name <span className="font-normal text-ink-muted">(optional)</span>
          </label>
          <input
            id={shortNameId}
            name="slug"
            type="text"
            autoComplete="off"
            value={shortName}
            onChange={(event) => {
              setShortName(event.target.value)
              if (invalid !== null) setInvalid(null)
            }}
            aria-invalid={invalid?.field === "slug" ? true : undefined}
            aria-describedby={
              invalid?.field === "slug" ? `${invalidId} ${shortNameHintId}` : shortNameHintId
            }
            className="block w-full rounded-sm border border-border-strong bg-surface-raised px-3 py-2.5 text-base text-ink"
          />
          <p id={shortNameHintId} className="text-sm text-ink-muted">
            Used in web addresses. Lowercase letters, numbers and hyphens. Left empty, we make one
            from the name.
          </p>
          {invalid?.field === "slug" ? (
            <p id={invalidId} className="text-sm text-ink">
              {invalid.message}
            </p>
          ) : null}
        </div>

        <button
          type="submit"
          className="rounded-sm bg-agent px-5 py-2.5 text-base font-medium text-agent-ink"
        >
          {/* The action keeps its name while it is happening (SPEC §18). */}
          {create.isPending ? "Creating organisation" : "Create organisation"}
        </button>
      </form>

      {create.isError ? (
        <ErrorNotice error={create.error}>
          <a href={routes.signIn} className="font-medium text-ink underline">
            Sign in
          </a>
        </ErrorNotice>
      ) : null}
    </div>
  )
}
