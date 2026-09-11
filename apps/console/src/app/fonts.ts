/**
 * The console's typefaces, from the approved mockup (`docs/mockup.html`).
 *
 * `next/font/google` downloads these at BUILD time and self-hosts them, so
 * nothing is fetched from fonts.gstatic.com at runtime. On Cloudflare Workers
 * that matters twice: a cold start pays no extra DNS lookup and TLS handshake,
 * and the sandbox's deny-by-default egress (SPEC §17.1) does not need a hole
 * opened for a font CDN.
 *
 * Budget, measured 2026-09-12 and recorded in `docs/verified.md`: 33.2 KB for
 * the variable 400–700 sans and 17.3 + 10.5 KB for mono 400/600 — 61 KB for
 * the whole type system, latin subset.
 *
 * `adjustFontFallback` is asked for but does NOT apply here, and that is worth
 * stating rather than leaving as a comfortable assumption. Next only generates
 * an adjusted fallback for families in its own metrics database, and neither
 * Atkinson family is in it — verified by building and grepping the output, in
 * which no `Fallback` face appears.
 *
 * So the metric-matched fallbacks are authored by hand in `globals.css`, from
 * metrics measured off the real fonts. That matters because `display: swap`
 * without them reflows the page when the webfont lands, and in this console
 * that lands on the credit gauge — top bar, every screen, a reserved slot for
 * every readout — which is exactly the jitter §18's persistent chrome must not
 * have. The option is left set in case a future Next version adds the metrics;
 * if it ever does, the hand-authored faces become redundant rather than wrong.
 */

import { Atkinson_Hyperlegible_Mono, Atkinson_Hyperlegible_Next } from "next/font/google"

export const sans = Atkinson_Hyperlegible_Next({
  subsets: ["latin"],
  // One variable file covers the four weights the mockup uses: 400 body,
  // 500 UI labels, 600 panel titles and the gauge readout, 700 page headings.
  weight: "variable",
  display: "swap",
  variable: "--font-sans-loaded",
  adjustFontFallback: true,
})

export const mono = Atkinson_Hyperlegible_Mono({
  subsets: ["latin"],
  // Mono ships static weights rather than a variable axis. 400 for measured
  // values, 600 for the gauge readout. No other weight is used, so no other
  // weight is downloaded.
  weight: ["400", "600"],
  display: "swap",
  variable: "--font-mono-loaded",
  adjustFontFallback: true,
})
