#!/usr/bin/env node
/**
 * Compile the schema documents into zod schemas for the console.
 *
 * Called by scripts/gen.sh; see that script's header for why generated output
 * is committed rather than ignored. `make gen-check` diffs everything under
 * packages/schema/gen, so these files are covered by the same drift job as the
 * TypeScript, Go and Python bindings.
 *
 * SPEC 3.1: "Forms: react-hook-form + zod, with zod schemas generated from
 * packages/schema". Task 0.3 generated types for three languages and no zod, so
 * the first form in the repository could not be written without hand-writing a
 * schema the documents already describe.
 *
 * Three things about this generator are worth knowing before changing it.
 *
 * 1. json-schema-to-zod does NOT resolve `$ref`. It is a structural converter:
 *    hand it a node with a `$ref` and every parser declines it, so it falls
 *    through to `z.any()` — silently, with no error and no warning. The library
 *    offers `parserOverride` for exactly this, and that is where every `$ref` in
 *    this repository's documents is turned into a reference to another emitted
 *    const. An unresolvable `$ref` throws here rather than becoming `z.any()`,
 *    because `z.any()` is a validator that accepts anything: a form built on one
 *    would appear to validate and would check nothing.
 *
 * 2. The emitted consts are plain `const`s evaluated at module load, so a const
 *    must appear before the ones that use it. Document order is not that order
 *    (AuthSession precedes AuthUser), so the output is topologically sorted, and
 *    a reference cycle is a hard error rather than a file that throws at import.
 *
 * 3. Determinism. Nothing is timestamped and every iteration order comes from
 *    the source document. A generator whose output varies between runs turns
 *    `make gen-check` into a coin flip that reviewers learn to re-run.
 *
 * 4. `format` keywords are zod's, not JSON Schema's, and zod's are stricter in
 *    one place worth knowing: `format: uuid` compiles to `z.uuid()`, which
 *    enforces the RFC version nibble, so `...-0a6b-...` is refused where
 *    common.schema.json's pattern alone would take it. Strictly tighter than
 *    the document, never looser, and the control plane mints v4 and v7 — but if
 *    a document ever needs a UUID-shaped identifier that is not a real UUID,
 *    this is where the surprise comes from.
 */

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import { basename, dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

import { jsonSchemaToZod } from "json-schema-to-zod"
import { format, resolveConfig } from "prettier"
import { parse as parseYaml } from "yaml"

const ROOT = fileURLToPath(new URL("../", import.meta.url))
const SCHEMA_DIR = join(ROOT, "packages/schema")
const OUT_DIR = join(ROOT, "packages/schema/gen/zod")

/**
 * The documents that get zod output, and nothing else.
 *
 * zod exists here to validate at a TypeScript boundary — a form before it is
 * submitted, a response body before it is trusted — so a document with no
 * TypeScript consumer would produce generated code nobody imports. `common` and
 * `api-v1` are what the console reads. `agent-events`, `capability-manifest`,
 * `meters` and `sandboxd-api` are read by Go and Python today; adding one is a
 * row in this table and a line in scripts/check-structure.sh, not a redesign.
 */
const DOCS = [
  { file: "common.schema.json", out: "common.ts", kind: "jsonschema" },
  { file: "api.openapi.yaml", out: "api-v1.ts", kind: "openapi" },
]

/** Methods in the order OpenAPI 3.1 lists them, so path iteration is stable. */
const HTTP_METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"]

const outputs = () => DOCS.map((d) => `packages/schema/gen/zod/${d.out}`)

// --------------------------------------------------------------- loading

function loadDocument(file) {
  const path = join(SCHEMA_DIR, file)
  const text = readFileSync(path, "utf8")
  return file.endsWith(".json") ? JSON.parse(text) : parseYaml(text)
}

function pointerGet(doc, pointer) {
  let node = doc
  for (const raw of pointer.split("/").slice(1)) {
    const key = raw.replace(/~1/g, "/").replace(/~0/g, "~")
    if (node === undefined || node === null) return undefined
    node = node[key]
  }
  return node
}

/** `Foo` -> `FooSchema`. One suffix, applied everywhere, so nothing shadows a
 * global: `common.schema.json` defines `Error`, and a module-level `const Error`
 * would shadow the built-in for every line below it. */
const exportName = (name) => `${name}Schema`

const pascal = (s) => s.charAt(0).toUpperCase() + s.slice(1)

// --------------------------------------------------------------- the type table
//
// Two maps per document:
//   types    exported const name -> the schema to convert, in emission order
//   byRef    JSON pointer -> exported const name, for parserOverride
//
// common.schema.json defines everything under $defs and aliases each one under
// components/schemas (its own header explains why: oapi-codegen rejects a
// `#/$defs/X` pointer). Both spellings therefore have to resolve to the same
// const, which is what the alias pass below does — without it, every `$ref`
// written by the OpenAPI documents would be unresolvable.

/**
 * Walk the schema keywords that can hold a subschema.
 *
 * Keyword-aware rather than "every object with a `title`": a schema is allowed
 * a property literally called `title` or `items`, and a blind walk would read
 * the property map as a schema.
 */
const SUBSCHEMA_MAPS = ["properties", "patternProperties", "$defs", "definitions"]
const SUBSCHEMA_LISTS = ["oneOf", "anyOf", "allOf", "prefixItems"]
const SUBSCHEMA_SINGLES = ["items", "additionalProperties", "not", "if", "then", "else", "contains"]

function* subschemas(node) {
  if (node === null || typeof node !== "object") return
  for (const key of SUBSCHEMA_MAPS) {
    for (const child of Object.values(node[key] ?? {})) {
      if (child !== null && typeof child === "object") yield child
    }
  }
  for (const key of SUBSCHEMA_LISTS) {
    for (const child of node[key] ?? []) {
      if (child !== null && typeof child === "object") yield child
    }
  }
  for (const key of SUBSCHEMA_SINGLES) {
    const child = node[key]
    if (child !== null && typeof child === "object") yield child
  }
}

/**
 * Subschemas that carry a `title`, which become exports of their own.
 *
 * The documents use `title` to name the members of a `oneOf`
 * (`CreateProjectFromTemplate`, `CreateProjectFromPrompt`) and one inline enum
 * (`LeaseAction`), and the Python generator already turns each into its own
 * class via `--use-title-as-name`. Doing the same here is what makes
 * `CreateProjectRequest` usable from a form: json-schema-to-zod compiles a
 * `oneOf` to `z.any().superRefine(...)`, whose inferred type is `any`, so a
 * console that only had the union would get no typing from it at all. It can
 * reach for the branch it is actually collecting instead.
 *
 * Descent stops at each titled node: its own titled children belong to it.
 */
function titledChildren(node, found = []) {
  for (const child of subschemas(node)) {
    if (typeof child.title === "string") found.push(child)
    else titledChildren(child, found)
  }
  return found
}

// --------------------------------------------------------------- the type table
//
// Per document:
//   types    exported const name -> the schema to convert, in document order
//   byRef    JSON pointer -> exported const name, for resolving $ref
//   byNode   schema object -> exported const name, for the titled subschemas
//            above, which nothing can $ref but which must not be inlined twice
//
// common.schema.json defines everything under $defs and aliases each one under
// components/schemas (its own header explains why: oapi-codegen rejects a
// `#/$defs/X` pointer). Both spellings therefore have to resolve to the same
// const, which is what the alias pass below does — without it, every `$ref`
// written by the OpenAPI documents would be unresolvable.

function collect(doc, spec) {
  const types = new Map()
  const byRef = new Map()
  const byNode = new Map()

  const declare = (name, schema) => {
    const exported = exportName(name)
    if (types.has(exported)) {
      throw new Error(`${spec.file}: two schemas would both be exported as ${exported}`)
    }
    types.set(exported, schema)
    return exported
  }

  const withTitledChildren = (name, schema) => {
    const exported = declare(name, schema)
    for (const child of titledChildren(schema)) {
      byNode.set(child, withTitledChildren(child.title, child))
    }
    return exported
  }

  if (spec.kind === "jsonschema") {
    for (const [name, schema] of Object.entries(doc.$defs ?? {})) {
      byRef.set(`#/$defs/${name}`, withTitledChildren(name, schema))
    }
    for (const [name, schema] of Object.entries(doc.components?.schemas ?? {})) {
      if (typeof schema?.$ref === "string" && byRef.has(schema.$ref)) {
        byRef.set(`#/components/schemas/${name}`, byRef.get(schema.$ref))
      }
    }
    return { types, byRef, byNode }
  }

  for (const [name, schema] of Object.entries(doc.components?.schemas ?? {})) {
    byRef.set(`#/components/schemas/${name}`, withTitledChildren(name, schema))
  }

  // Request bodies the document writes inline rather than naming.
  //
  // POST /v1/auth/magic-link is one of these: its body is `{ email }` written
  // straight into the operation, so there is no component name to reuse and the
  // console has nothing to import unless it is picked up here. The shape is the
  // contract's, verbatim — only the NAME is ours, derived from the operation's
  // own operationId, and the `Body` suffix marks it as such. Nothing in the
  // documents can `$ref` one of these, so they are deliberately absent from
  // byRef.
  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      const op = item?.[method]
      const schema = op?.requestBody?.content?.["application/json"]?.schema
      if (!schema || typeof schema.$ref === "string") continue
      if (typeof op.operationId !== "string") {
        throw new Error(
          `${spec.file}: ${method.toUpperCase()} ${path} has an inline request body ` +
            `and no operationId to name it after`,
        )
      }
      withTitledChildren(`${pascal(op.operationId)}Body`, schema)
    }
  }
  return { types, byRef, byNode }
}

// --------------------------------------------------------------- references

/**
 * Resolve one `$ref` to the const it should compile to.
 *
 * Handles the three spellings the documents use: `#/$defs/X` (JSON Schema
 * documents referring to themselves), `#/components/schemas/X` (OpenAPI
 * documents referring to themselves) and `./common.schema.json#/...` or
 * `common.schema.json#/...` (either kind referring to common).
 */
function makeResolver(docs, selfId) {
  return (ref, where) => {
    const [file, pointer] = ref.split("#")
    const id = file === "" ? selfId : basename(file)
    const target = docs.get(id)
    if (!target) {
      throw new Error(
        `${selfId}: ${where} refers to ${ref}, but ${id} has no zod output.\n` +
          `  Add it to DOCS in scripts/gen_zod.mjs, or the reference compiles to nothing.`,
      )
    }
    const name = target.byRef.get(`#${pointer}`)
    if (!name) {
      throw new Error(`${selfId}: ${where} refers to ${ref}, which names no schema in ${id}`)
    }
    return { id, name }
  }
}

/**
 * What one emitted const needs declared before it: the `$ref`s inside it, and
 * the titled subschemas that were lifted out into their own consts.
 *
 * The walk is deliberately generic rather than keyword-aware — a `$ref` missed
 * here is a const used before its declaration — and stops at any node that has
 * an export of its own, whose contents belong to that export instead.
 */
function dependenciesOf(root, byNode, onRef) {
  const seen = []
  const walk = (node) => {
    if (Array.isArray(node)) {
      for (const item of node) walk(item)
      return
    }
    if (node === null || typeof node !== "object") return
    if (node !== root && byNode.has(node)) {
      if (!seen.includes(byNode.get(node))) seen.push(byNode.get(node))
      return
    }
    for (const [key, value] of Object.entries(node)) {
      if (key === "$ref" && typeof value === "string") {
        const name = onRef(value)
        if (name && !seen.includes(name)) seen.push(name)
      } else {
        walk(value)
      }
    }
  }
  walk(root)
  return seen
}

/**
 * Document order, rearranged so a const is declared before it is used.
 *
 * Depth-first over each type's own references, which keeps the result stable:
 * the same document always produces the same order.
 */
function topoSort(types, dependencies) {
  const ordered = []
  const state = new Map()
  const visit = (name, stack) => {
    if (state.get(name) === "done") return
    if (state.get(name) === "visiting") {
      throw new Error(
        `reference cycle: ${[...stack, name].join(" -> ")}.\n` +
          `  These compile to plain consts, so a cycle is a file that throws on import.\n` +
          `  Breaking one needs z.lazy() and an explicit type annotation; do it deliberately.`,
      )
    }
    state.set(name, "visiting")
    for (const dep of dependencies.get(name) ?? []) visit(dep, [...stack, name])
    state.set(name, "done")
    ordered.push(name)
  }
  for (const name of types.keys()) visit(name, [])
  return ordered
}

// --------------------------------------------------------------- emission

const COMMENT_WIDTH = 76

/** Wrap a description into JSDoc lines. Blank lines in the source survive. */
function jsdoc(description) {
  if (!description) return ""
  const out = []
  for (const paragraph of String(description).trimEnd().split("\n")) {
    const text = paragraph.trim()
    if (text === "") {
      out.push("")
      continue
    }
    let line = ""
    for (const word of text.split(/\s+/)) {
      if (line === "") line = word
      else if (line.length + 1 + word.length <= COMMENT_WIDTH) line += ` ${word}`
      else {
        out.push(line)
        line = word
      }
    }
    out.push(line)
  }
  const body = out.map((l) => (l === "" ? " *" : ` * ${l.replace(/\*\//g, "*\\/")}`)).join("\n")
  return `/**\n${body}\n */\n`
}

function header(spec) {
  return `// Code generated by scripts/gen_zod.mjs from packages/schema/${spec.file}. DO NOT EDIT.
//
// Editing this file is pointless: the next \`make gen\` overwrites it, and
// \`make gen-check\` fails the build in the meantime. Change the schema instead.
//
// SPEC 3.1 puts console forms on react-hook-form + zod "with zod schemas
// generated from packages/schema". These are those schemas. The matching
// TypeScript types come from @halyard/schema/api-v1 and @halyard/schema/common,
// which are generated from the same documents: never restate a shape by hand.
`
}

async function compile(spec, docs) {
  const { types, byNode } = docs.get(spec.file)
  const resolve = makeResolver(docs, spec.file)

  // Resolve every reference before compiling anything, and let main() write
  // only once every document has compiled: a run that dies half way through
  // would otherwise leave the tree with one new file and one stale one, which
  // `make gen-check` reports as drift in a schema nobody touched.
  const dependencies = new Map()
  const imported = new Map()
  for (const [name, schema] of types) {
    dependencies.set(
      name,
      dependenciesOf(schema, byNode, (ref) => {
        const target = resolve(ref, name)
        if (target.id === spec.file) return target.name
        const module = docs.get(target.id).spec.out.replace(/\.ts$/, "")
        if (!imported.has(module)) imported.set(module, new Set())
        imported.get(module).add(target.name)
        return undefined
      }),
    )
  }

  const chunks = [header(spec), `import { z } from "zod"\n`]
  for (const [module, names] of [...imported].sort()) {
    chunks.push(`import { ${[...names].sort().join(", ")} } from "./${module}"\n`)
  }
  chunks.push("\n")

  for (const name of topoSort(types, dependencies)) {
    const schema = types.get(name)
    // parserOverride is how a `$ref` becomes a reference to another const
    // rather than `z.any()`, and how a titled subschema becomes a reference to
    // its own const rather than being inlined a second time. It fires on every
    // node INCLUDING the root, which for a lifted subschema is the very node
    // being compiled — hence the identity guard, without which each of those
    // would compile to `const XSchema = XSchema`.
    const expression = jsonSchemaToZod(schema, {
      module: "none",
      zodVersion: 4,
      // Descriptions travel as JSDoc instead: `.describe()` would bury multiple
      // paragraphs of SPEC prose inside a single-line expression, where the
      // reader who needs it is the one hovering the symbol in an editor.
      withoutDescribes: true,
      parserOverride: (node) => {
        if (node === schema) return undefined
        if (byNode.has(node)) return byNode.get(node)
        if (typeof node?.$ref !== "string") return undefined
        return resolve(node.$ref, name).name
      },
    })
    chunks.push(`${jsdoc(schema.description)}export const ${name} = ${expression}\n\n`)
  }

  const file = join(OUT_DIR, spec.out)
  const prettierConfig = await resolveConfig(file)
  // gen/ is in .prettierignore, so `make lint-js` never formats this file; the
  // formatting has to happen here or the output is one 400-column line per type.
  const contents = await format(chunks.join(""), {
    ...prettierConfig,
    filepath: file,
    parser: "typescript",
  })
  return { file, contents, out: spec.out, count: types.size }
}

// --------------------------------------------------------------- main

if (process.argv.includes("--outputs")) {
  console.log(outputs().join("\n"))
  process.exit(0)
}

if (!existsSync(dirname(OUT_DIR))) {
  throw new Error(`${dirname(OUT_DIR)} does not exist; run this through scripts/gen.sh`)
}
mkdirSync(OUT_DIR, { recursive: true })

const docs = new Map()
for (const spec of DOCS) {
  const document = loadDocument(spec.file)
  docs.set(spec.file, { spec, document, ...collect(document, spec) })
}

const compiled = []
for (const spec of DOCS) compiled.push(await compile(spec, docs))

for (const { file, contents, out, count } of compiled) {
  writeFileSync(file, contents)
  console.log(`      ${out} (${count} schemas)`)
}
