import type { ReactNode } from "react"
import { TabNav } from "@/components/tab-nav"
import { settingsNav } from "@/lib/routes"

/**
 * Account settings: the three screens SPEC §18 gives them.
 *
 * These are org-scoped rather than project-scoped — credits are bought by an
 * organisation and spent by its projects (SPEC §16), and team membership is an
 * org fact (SPEC §8) — which is why they sit outside /p/[project] and keep
 * their own nav.
 */
export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-4xl px-5 py-5">
      <TabNav label="Settings" items={settingsNav} />
      <div className="space-y-6 py-6">{children}</div>
    </div>
  )
}
