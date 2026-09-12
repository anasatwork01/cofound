/**
 * What the generated zod schemas actually accept and reject, and whether they
 * still describe the same shapes as the TypeScript bindings generated from the
 * same documents.
 *
 * Task 0.14. These are the three bodies phase 0's acceptance criterion needs —
 * sign in, create an org, create a project — plus the type agreement that stops
 * the two generators drifting apart underneath a form.
 */
import { describe, expect, it } from "vitest"
import type { z } from "zod"

import type { components, operations } from "../gen/typescript/api-v1"
import {
  ChangeMemberRoleBodySchema,
  CreateInviteRequestSchema,
  CreateOrgRequestSchema,
  CreateProjectFromPromptSchema,
  CreateProjectFromTemplateSchema,
  CreateProjectRequestSchema,
  RequestMagicLinkBodySchema,
  VerifyMagicLinkBodySchema,
} from "../gen/zod/api-v1"
import { ErrorResponseSchema, SlugSchema } from "../gen/zod/common"

const UUID = "b0a1c2d3-4e5f-4a6b-8c9d-0e1f2a3b4c5d"

describe("CreateOrgRequest", () => {
  it("accepts a name on its own; SPEC 8 lets the server derive the slug", () => {
    expect(CreateOrgRequestSchema.safeParse({ name: "Acme" }).success).toBe(true)
    expect(CreateOrgRequestSchema.safeParse({ name: "Acme", slug: "acme" }).success).toBe(true)
  })

  it("rejects what the document says is invalid", () => {
    const rejected = [
      {}, // name is required
      { name: "" }, // minLength 1
      { name: "a".repeat(201) }, // maxLength 200
      { name: "Acme", slug: "Acme Inc" }, // uppercase and a space are not DNS-label safe
      { name: "Acme", slug: "-acme" }, // a slug reaches a preview hostname (SPEC 9)
      { name: "Acme", slug: "a".repeat(64) }, // maxLength 63
      { name: "Acme", plan: "enterprise" }, // additionalProperties: false
    ]
    for (const body of rejected) {
      expect(CreateOrgRequestSchema.safeParse(body).success, JSON.stringify(body)).toBe(false)
    }
  })
})

describe("requestMagicLink body", () => {
  it("accepts an address", () => {
    expect(RequestMagicLinkBodySchema.safeParse({ email: "ada@example.com" }).success).toBe(true)
  })

  it("rejects what the document says is invalid", () => {
    const rejected = [
      {},
      { email: "" },
      { email: "not-an-address" },
      { email: `${"a".repeat(320)}@example.com` }, // maxLength 320
      { email: "ada@example.com", next: "/" }, // additionalProperties: false
    ]
    for (const body of rejected) {
      expect(RequestMagicLinkBodySchema.safeParse(body).success, JSON.stringify(body)).toBe(false)
    }
  })
})

describe("verifyMagicLink body", () => {
  it("pins the token to the 43 characters the document specifies", () => {
    expect(VerifyMagicLinkBodySchema.safeParse({ token: "t".repeat(43) }).success).toBe(true)
    expect(VerifyMagicLinkBodySchema.safeParse({ token: "t".repeat(42) }).success).toBe(false)
    expect(VerifyMagicLinkBodySchema.safeParse({ token: "t".repeat(44) }).success).toBe(false)
  })
})

describe("CreateProjectRequest", () => {
  const fromTemplate = { org_id: UUID, name: "Shop", template_version_id: UUID }
  const fromPrompt = { org_id: UUID, prompt: "a shop that sells rope" }

  it("takes either a template version or a prompt", () => {
    expect(CreateProjectRequestSchema.safeParse(fromTemplate).success).toBe(true)
    expect(CreateProjectRequestSchema.safeParse(fromPrompt).success).toBe(true)
  })

  it("refuses both at once and neither, which is what oneOf means (SPEC 7.1)", () => {
    expect(CreateProjectRequestSchema.safeParse({ ...fromTemplate, ...fromPrompt }).success).toBe(
      false,
    )
    expect(CreateProjectRequestSchema.safeParse({ org_id: UUID, name: "Shop" }).success).toBe(false)
  })

  /**
   * The union compiles to `z.any().superRefine(...)`, so its inferred type is
   * `any` and a form typed from it would be typed from nothing. The two titled
   * branches are exported separately for exactly that reason: a form collects
   * one of them, not the union.
   */
  it("exports each branch as a schema a form can be built on", () => {
    expect(CreateProjectFromTemplateSchema.safeParse(fromTemplate).success).toBe(true)
    expect(CreateProjectFromTemplateSchema.safeParse(fromPrompt).success).toBe(false)
    expect(CreateProjectFromPromptSchema.safeParse(fromPrompt).success).toBe(true)
    expect(CreateProjectFromPromptSchema.safeParse(fromTemplate).success).toBe(false)
  })

  it("carries the org_id and length bounds into both branches", () => {
    expect(CreateProjectFromPromptSchema.safeParse({ ...fromPrompt, org_id: "acme" }).success).toBe(
      false,
    )
    expect(CreateProjectFromPromptSchema.safeParse({ ...fromPrompt, prompt: "" }).success).toBe(
      false,
    )
    expect(
      CreateProjectFromPromptSchema.safeParse({ ...fromPrompt, prompt: "x".repeat(20001) }).success,
    ).toBe(false)
  })
})

describe("references across documents", () => {
  /**
   * `Role` and `Slug` live in common.schema.json and are referenced by
   * api.openapi.yaml. If the generator had dropped the reference they would be
   * `z.any()`, and this is what that would look like from the outside: a member
   * role of "wizard" accepted without complaint.
   */
  it("resolves a common.schema.json role rather than accepting anything", () => {
    expect(ChangeMemberRoleBodySchema.safeParse({ role: "admin" }).success).toBe(true)
    expect(ChangeMemberRoleBodySchema.safeParse({ role: "wizard" }).success).toBe(false)
    expect(ChangeMemberRoleBodySchema.safeParse({ role: 7 }).success).toBe(false)
  })

  it("resolves the slug pattern inside a nested object", () => {
    expect(SlugSchema.safeParse("preview-1").success).toBe(true)
    expect(SlugSchema.safeParse("Preview 1").success).toBe(false)
  })

  it("parses the error envelope SPEC 10 requires", () => {
    const body = { error: { code: "ambiguous_project", message: "Two orgs.", retriable: false } }
    expect(ErrorResponseSchema.safeParse(body).success).toBe(true)
    expect(ErrorResponseSchema.safeParse({ error: { code: "Nope", message: "x" } }).success).toBe(
      false,
    )
  })
})

/*
 * Type agreement, checked by `make typecheck` rather than at run time.
 *
 * Two generators read the same document: openapi-typescript produces the types
 * the console's fetchers are written against, scripts/gen_zod.mjs produces the
 * schemas its forms are written against. Nothing makes them agree except that
 * they read the same source, and a console that types a form from one and posts
 * it through the other would not notice them diverging until the API answered
 * 422.
 *
 * `Undef` is why this is not a bare `Exact`: zod infers an optional property as
 * `T | undefined` and openapi-typescript writes it as `T`, which
 * exactOptionalPropertyTypes makes a genuine difference and which says nothing
 * about the contract. Optionality itself still has to match — the mapped type
 * preserves `?`, so a property required on one side and optional on the other
 * still fails.
 */
type Undef<T> = { [K in keyof T]: T[K] | undefined }
type Exact<A, B> = [Undef<A>] extends [Undef<B>]
  ? [Undef<B>] extends [Undef<A>]
    ? true
    : false
  : false

const _createOrgAgrees: Exact<
  z.infer<typeof CreateOrgRequestSchema>,
  components["schemas"]["CreateOrgRequest"]
> = true

/*
 * Taken through `operations` rather than `components`, because this body is
 * written inline in the operation and has no component name — which is the
 * whole reason scripts/gen_zod.mjs names it after the operationId. Restating
 * `{ email: string }` here instead would be the hand-written duplicate that
 * CLAUDE.md working agreement 1 forbids, inside the test that exists to prove
 * there is not one.
 */
const _magicLinkAgrees: Exact<
  z.infer<typeof RequestMagicLinkBodySchema>,
  operations["requestMagicLink"]["requestBody"]["content"]["application/json"]
> = true

const _fromTemplateAgrees: Exact<
  z.infer<typeof CreateProjectFromTemplateSchema>,
  Extract<components["schemas"]["CreateProjectRequest"], { template_version_id: string }>
> = true

const _fromPromptAgrees: Exact<
  z.infer<typeof CreateProjectFromPromptSchema>,
  Extract<components["schemas"]["CreateProjectRequest"], { prompt: string }>
> = true

const _createInviteAgrees: Exact<
  z.infer<typeof CreateInviteRequestSchema>,
  components["schemas"]["CreateInviteRequest"]
> = true

/*
 * `ErrorResponse` is deliberately absent from this list. `Error.details` is
 * `{"type": "object"}` with no properties, which openapi-typescript renders as
 * `Record<string, never>` — an object that can hold nothing — and zod as
 * `z.record(z.string(), z.any())`. The schema means "code-specific context"
 * (SPEC §10), so zod has it right and the two cannot be made to agree without
 * changing the document. Nothing posts an error envelope, so nothing is at
 * risk; this note exists so the divergence is not later read as a bug in the
 * generator.
 */

void [
  _createOrgAgrees,
  _magicLinkAgrees,
  _fromTemplateAgrees,
  _fromPromptAgrees,
  _createInviteAgrees,
]
