import { defineCloudflareConfig } from "@opennextjs/cloudflare"

/**
 * Bare on purpose.
 *
 * `defineCloudflareConfig()` defaults `incrementalCache`, `tagCache`, `queue`
 * and `cachePurge` to "dummy" and `enableCacheInterception` to false. None of
 * it is required unless ISR or on-demand revalidation is actually used, and the
 * console has neither — it is an authenticated dashboard rendered per request.
 *
 * So task 0.12 provisions no R2 bucket, no KV namespace, no D1 database and no
 * Durable Object queue for the console. The generated user apps (tasks 3.1 and
 * 3.3) are a different story entirely and will need most of that; see
 * docs/verified.md, SPEC §22 item 1.
 */
export default defineCloudflareConfig({})
