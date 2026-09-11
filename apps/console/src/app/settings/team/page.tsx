import type { Metadata } from "next"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"

export const metadata: Metadata = { title: "Team" }

/**
 * Members, invites, roles. Task 0.9 ships the API (org and project CRUD,
 * invites, org switching, the audit log) and this screen renders it.
 *
 * SPEC §8's four roles decide what each member can do, including who can
 * approve a proposal the agent raises — which is why the roles are named on
 * this screen rather than hidden behind a dropdown.
 */
export default function TeamPage() {
  return (
    <>
      <PageHeader
        title="Team"
        lede="Who is in this organisation, and what each of them is allowed to do."
      />
      <EmptyState
        title="You are the only member"
        body="Invite the people you build with. You choose what each of them can do — from reading along to approving what the agent asks for."
      />
    </>
  )
}
