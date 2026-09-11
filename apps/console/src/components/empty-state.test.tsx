import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { EmptyState } from "@/components/empty-state"

describe("EmptyState", () => {
  it("names what is missing and what fills it", () => {
    render(
      <EmptyState
        title="No changes yet"
        body="Each thing you ask for is kept as a version you can restore."
        action={{ href: "/new", label: "Start a project" }}
      />,
    )
    expect(screen.getByRole("heading", { name: "No changes yet" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Start a project" })).toHaveAttribute("href", "/new")
  })

  it("renders no action when there is nowhere real to go", () => {
    render(<EmptyState title="No charges yet" body="Every charge lands here." />)
    expect(screen.queryByRole("link")).not.toBeInTheDocument()
  })
})
