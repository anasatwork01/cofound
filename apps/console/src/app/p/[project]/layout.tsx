import type { ReactNode } from "react"
import { TabNav } from "@/components/tab-nav"
import { projectNav } from "@/lib/routes"

/**
 * The project shell: the seven screens SPEC §18 gives a project, and the frame
 * they share.
 *
 * Async because `params` is a promise in this version of Next. That makes it
 * an async Server Component, which Next documents as unsupported in unit
 * tests — and the failure is silent, so there is deliberately no unit test for
 * this file. What it renders is covered by `tab-nav.test.tsx` (the nav) and
 * task 1.18's end-to-end test (the whole screen).
 *
 * SEAM: the project's name and its org belong in the top bar's picker, which
 * is the other half of task 0.11. This layout deliberately does not fetch the
 * project — nothing on these screens needs it yet, and a fetch here would run
 * on every navigation between the seven tabs.
 */
export default async function ProjectLayout({
  children,
  params,
}: {
  children: ReactNode
  params: Promise<{ project: string }>
}) {
  const { project } = await params

  return (
    <div className="mx-auto w-full max-w-6xl px-5 py-5">
      <TabNav label="Project" items={projectNav(project)} />
      <div className="space-y-6 py-6">{children}</div>
    </div>
  )
}
