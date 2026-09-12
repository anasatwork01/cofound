/**
 * Guards on the zod generator itself, as opposed to what it produced.
 *
 * Task 0.14. SPEC §3.1 puts console forms on "react-hook-form + zod, with zod
 * schemas generated from `packages/schema`", and scripts/gen_zod.mjs is how
 * that happens. Two of its failure modes are silent, which is why they are
 * asserted here rather than left to review.
 */
import { execFileSync } from "node:child_process"
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"

import { parse as parseYaml } from "yaml"
import { describe, expect, it } from "vitest"

const ROOT = fileURLToPath(new URL("../../../", import.meta.url))
const read = (path: string) => readFileSync(ROOT + path, "utf8")

const GENERATED = ["packages/schema/gen/zod/common.ts", "packages/schema/gen/zod/api-v1.ts"]

describe("scripts/gen_zod.mjs output", () => {
  /**
   * The failure this whole generator exists to prevent.
   *
   * json-schema-to-zod has no `$ref` support at all: every parser declines a
   * node carrying one and it falls through to `z.any()` — no error, no warning,
   * no hint in the output that a constraint was dropped. `z.any()` accepts
   * everything, so a form built on one would look validated and would check
   * nothing, and the first sign of trouble would be a 422 from `api`.
   *
   * scripts/gen_zod.mjs resolves references itself and throws on one it cannot
   * place. This asserts the outcome: the only `z.any()` allowed in the output is
   * one of the two that genuinely means "anything".
   */
  it("contains no z.any() that is not a deliberate one", () => {
    for (const path of GENERATED) {
      const source = read(path)
      const offenders: string[] = []
      for (const match of source.matchAll(/z\.any\(\)/g)) {
        const at = match.index
        const after = source.slice(at + "z.any()".length)
        const before = source.slice(0, at)
        // `oneOf` compiles to `z.any().superRefine(...)`, which then runs each
        // branch: the `any` is the input to a real check, not the check.
        if (after.startsWith(".superRefine(")) continue
        // `Error.details` is `{"type": "object"}` with no properties — SPEC §10
        // leaves it code-specific on purpose — so its values really are free.
        if (before.endsWith("z.record(z.string(), ")) continue
        offenders.push(source.slice(Math.max(0, at - 60), at + 20).replace(/\s+/g, " "))
      }
      expect(offenders, `${path} has a z.any() that accepts anything`).toEqual([])
    }
  })

  /**
   * scripts/check-structure.sh lists these paths literally, because it runs in a
   * CI job with no node_modules and gen_zod.mjs cannot be imported there. A list
   * copied by hand is a list that goes stale, so the copy is checked here — in
   * the job that does have node_modules.
   */
  it("declares the same outputs scripts/check-structure.sh checks for", () => {
    const declared = execFileSync("node", ["scripts/gen_zod.mjs", "--outputs"], {
      cwd: ROOT,
      encoding: "utf8",
    })
      .trim()
      .split("\n")

    const block = /zod_outputs=\(([^)]*)\)/.exec(read("scripts/check-structure.sh"))?.[1]
    expect(block, "scripts/check-structure.sh no longer has a zod_outputs array").toBeTypeOf(
      "string",
    )
    const listed = (block ?? "").trim().split(/\s+/)

    expect(listed).toEqual(declared)
    expect(declared).toEqual(GENERATED)
  })

  /**
   * Every schema the API contract names is reachable from TypeScript. A missing
   * one is not a compile error anywhere — the console simply cannot import it,
   * and the pressure is then to hand-write the shape, which is the thing
   * CLAUDE.md working agreement 1 forbids.
   */
  it("exports a const for every named schema in api.openapi.yaml", () => {
    const doc = parseYaml(read("packages/schema/api.openapi.yaml")) as {
      components: { schemas: Record<string, unknown> }
    }
    const source = read("packages/schema/gen/zod/api-v1.ts")
    const missing = Object.keys(doc.components.schemas).filter(
      (name) => !source.includes(`export const ${name}Schema =`),
    )
    expect(missing).toEqual([])
  })

  /**
   * The same for `common.schema.json`, whose definitions the OpenAPI documents
   * reference rather than redefine.
   */
  it("exports a const for every definition in common.schema.json", () => {
    const doc = JSON.parse(read("packages/schema/common.schema.json")) as {
      $defs: Record<string, unknown>
    }
    const source = read("packages/schema/gen/zod/common.ts")
    const missing = Object.keys(doc.$defs).filter(
      (name) => !source.includes(`export const ${name}Schema =`),
    )
    expect(missing).toEqual([])
  })

  /**
   * A request body written inline in the document, rather than as a named
   * component, still has to reach the console: `POST /v1/auth/magic-link` is
   * the sign-in form task 0.14 needs and its body is three lines of YAML inside
   * the operation. The name is derived from the operation's own operationId.
   */
  it("exports a const for every inline JSON request body in api.openapi.yaml", () => {
    const doc = parseYaml(read("packages/schema/api.openapi.yaml")) as {
      paths: Record<string, Record<string, { operationId?: string; requestBody?: unknown }>>
    }
    const source = read("packages/schema/gen/zod/api-v1.ts")

    const inline: string[] = []
    for (const item of Object.values(doc.paths)) {
      for (const operation of Object.values(item)) {
        const body = (
          operation?.requestBody as
            { content?: Record<string, { schema?: { $ref?: string } }> } | undefined
        )?.content?.["application/json"]?.schema
        if (!body || body.$ref) continue
        const id = operation.operationId!
        inline.push(id.charAt(0).toUpperCase() + id.slice(1))
      }
    }

    expect(inline.length, "no inline request bodies found; the walk is wrong").toBeGreaterThan(0)
    expect(inline.filter((name) => !source.includes(`export const ${name}BodySchema =`))).toEqual(
      [],
    )
  })

  /**
   * The consts are plain values evaluated at module load, so one used before it
   * is declared is a ReferenceError at import time — not at parse time, and not
   * in any test that only reads the file. Document order is not declaration
   * order (AuthSession is written before AuthUser), so the generator sorts.
   */
  it("declares every const before it is used", async () => {
    await expect(import("../gen/zod/api-v1")).resolves.toBeTruthy()
    await expect(import("../gen/zod/common")).resolves.toBeTruthy()
  })
})
