import { beforeEach, describe, expect, it } from "vitest"
import {
  selectEditorMode,
  selectShowLeaseBanner,
  selectTakeoverPending,
  UNHELD_LEASE,
  useBuilderStore,
  type LeaseHolder,
} from "@/store/builder-store"

beforeEach(() => {
  useBuilderStore.getState().reset()
})

describe("open file", () => {
  it("opens and closes", () => {
    useBuilderStore.getState().openFile("src/app/page.tsx")
    expect(useBuilderStore.getState().openFilePath).toBe("src/app/page.tsx")
    useBuilderStore.getState().closeFile()
    expect(useBuilderStore.getState().openFilePath).toBeNull()
  })
})

describe("panel layout", () => {
  it("toggles one panel without disturbing the other", () => {
    useBuilderStore.getState().togglePanel("tree")
    expect(useBuilderStore.getState().panels).toEqual({ tree: false, preview: true })
    useBuilderStore.getState().setPanel("tree", true)
    expect(useBuilderStore.getState().panels).toEqual({ tree: true, preview: true })
  })
})

describe("lease", () => {
  it("starts held by nobody, so the editor is yours", () => {
    const state = useBuilderStore.getState()
    expect(state.lease).toEqual(UNHELD_LEASE)
    expect(selectEditorMode(state)).toBe("editable")
    expect(selectShowLeaseBanner(state)).toBe(false)
  })

  it("goes read-only when the agent takes it", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    expect(selectEditorMode(useBuilderStore.getState())).toBe("read-only")
  })

  it("is never read-only without a banner to explain it", () => {
    const holders: LeaseHolder[] = ["agent", "human", "none"]
    for (const holder of holders) {
      useBuilderStore.getState().applyLease({ holder })
      const state = useBuilderStore.getState()
      expect(selectShowLeaseBanner(state)).toBe(selectEditorMode(state) === "read-only")
    }
  })

  it("records a takeover as pending rather than seizing the lease", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    useBuilderStore.getState().requestTakeover()
    const state = useBuilderStore.getState()
    expect(state.lease.holder).toBe("agent")
    expect(state.lease.pending_holder).toBe("human")
    expect(selectTakeoverPending(state)).toBe(true)
    expect(selectEditorMode(state)).toBe("read-only")
  })

  it("ignores a takeover when the agent does not hold the lease", () => {
    useBuilderStore.getState().applyLease({ holder: "human" })
    useBuilderStore.getState().requestTakeover()
    expect(useBuilderStore.getState().lease).toEqual({ holder: "human" })
  })

  it("asking twice does not change anything", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    useBuilderStore.getState().requestTakeover()
    const first = useBuilderStore.getState().lease
    useBuilderStore.getState().requestTakeover()
    expect(useBuilderStore.getState().lease).toBe(first)
  })

  it("hands the editor back when the server grants the takeover", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    useBuilderStore.getState().requestTakeover()
    useBuilderStore.getState().applyLease({ holder: "human" })
    const state = useBuilderStore.getState()
    expect(selectEditorMode(state)).toBe("editable")
    expect(selectShowLeaseBanner(state)).toBe(false)
    expect(selectTakeoverPending(state)).toBe(false)
  })

  it("drops a pending takeover the server did not honour", () => {
    useBuilderStore.getState().applyLease({ holder: "agent" })
    useBuilderStore.getState().requestTakeover()
    useBuilderStore.getState().applyLease({ holder: "agent" })
    expect(selectTakeoverPending(useBuilderStore.getState())).toBe(false)
  })
})

describe("reset", () => {
  it("clears everything when the open project changes", () => {
    useBuilderStore.getState().openFile("README.md")
    useBuilderStore.getState().togglePanel("preview")
    useBuilderStore.getState().applyLease({ holder: "agent", pending_holder: "human" })
    useBuilderStore.getState().reset()
    const state = useBuilderStore.getState()
    expect(state.openFilePath).toBeNull()
    expect(state.panels).toEqual({ tree: true, preview: true })
    expect(state.lease).toEqual(UNHELD_LEASE)
  })
})
