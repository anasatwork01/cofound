import "./globals.css"
import type { Metadata } from "next"
import type { ReactNode } from "react"
// SEAM: the top bar is the other half of task 0.11 and ships from the shared
// package. SPEC §18 puts the credit gauge in it on EVERY screen, which is why
// it is mounted here rather than per route. There is no credits endpoint yet —
// `api.openapi.yaml` puts credits in phase 4 — so the gauge shows an explicit
// unknown state until task 4.9 gives it something real to read.
import { TopBar } from "@halyard/ui"
import { mono, sans } from "./fonts"
import { Providers } from "@/providers/providers"

export const metadata: Metadata = {
  title: { default: "Halyard", template: "%s · Halyard" },
  description: "Describe the app you want. Watch it get built, shipped and grown.",
}

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    // The font variables carry the loaded families down to the token layer,
    // which names them in --font-sans / --font-mono with a metric-matched
    // fallback behind each.
    <html lang="en" className={`${sans.variable} ${mono.variable}`}>
      <body className="min-h-dvh bg-surface font-sans text-ink">
        <Providers>
          {/* SPEC §18: keyboard-navigable throughout. Without this, reaching
              the page content means tabbing the whole top bar on every screen. */}
          <a
            href="#main"
            className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:border focus:border-border focus:bg-surface-raised focus:px-3 focus:py-2 focus:text-sm focus:font-medium"
          >
            Skip to content
          </a>

          <TopBar />

          <main id="main">{children}</main>

          {/* SPEC §18: `aria-live` for toasts. The region has to exist in the
              DOM before anything is put into it, or a screen reader announces
              nothing — so it is mounted empty here and task 1.16's toasts
              ("Publish" → "Published") render into it. */}
          <div
            id="toast-region"
            role="status"
            aria-live="polite"
            className="pointer-events-none fixed inset-x-0 bottom-0 z-40 flex flex-col items-center gap-2 p-4"
          />
        </Providers>
      </body>
    </html>
  )
}
