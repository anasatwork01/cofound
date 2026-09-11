"use client"

/**
 * The builder shell's local state, and nothing else.
 *
 * SPEC §3.1 is exact about the scope: "Zustand for the builder shell (open
 * file, lease state, panel layout). Nothing else needs global state." Server
 * data lives in TanStack Query, form state lives in the form. If you are about
 * to add a field here, check it is one of those three things first.
 *
 * The lease shape is `Lease` from `@halyard/schema/api-v1`, the same object the
 * REST endpoint and the `lease.changed` event carry, so there is no second
 * model of the lease to keep in step.
 */
import type { components } from "@halyard/schema/api-v1"
import { create } from "zustand"

export type Lease = components["schemas"]["Lease"]
export type LeaseHolder = components["schemas"]["LeaseHolder"]

/** The two panels either side of the chat column in the builder shell. */
export type PanelName = "tree" | "preview"

/** Nobody holds the lease: the editor is yours, and no banner is shown. */
export const UNHELD_LEASE: Lease = { holder: "none" }

export type BuilderState = {
  /** Path of the file the editor has open, or null when none is. Task 2.7. */
  openFilePath: string | null
  lease: Lease
  panels: Record<PanelName, boolean>
  openFile: (path: string) => void
  closeFile: () => void
  setPanel: (panel: PanelName, open: boolean) => void
  togglePanel: (panel: PanelName) => void
  /**
   * The server's view of the lease, from `GET .../lease` or from a
   * `lease.changed` event. Authoritative: it replaces whatever is here,
   * including an optimistic takeover the agent's turn has since resolved.
   */
  applyLease: (lease: Lease) => void
  /**
   * SPEC §18: the "take over" button "requests the lease for the end of the
   * current turn". It does not seize it, so this records a pending holder and
   * the banner says so; the handover arrives as a `lease.changed` event.
   */
  requestTakeover: () => void
  /** Called when the open project changes, so one project's shell does not leak into the next. */
  reset: () => void
}

const initialState = {
  openFilePath: null,
  lease: UNHELD_LEASE,
  panels: { tree: true, preview: true },
} satisfies Pick<BuilderState, "openFilePath" | "lease" | "panels">

export const useBuilderStore = create<BuilderState>()((set) => ({
  ...initialState,

  openFile: (path) => set({ openFilePath: path }),
  closeFile: () => set({ openFilePath: null }),

  setPanel: (panel, open) => set((state) => ({ panels: { ...state.panels, [panel]: open } })),
  togglePanel: (panel) =>
    set((state) => ({ panels: { ...state.panels, [panel]: !state.panels[panel] } })),

  applyLease: (lease) => set({ lease }),
  requestTakeover: () =>
    set((state) => {
      // Only meaningful while the agent holds it. Asking twice is not an error,
      // it just does not queue a second request.
      if (state.lease.holder !== "agent" || state.lease.pending_holder === "human") return {}
      return { lease: { ...state.lease, pending_holder: "human" } }
    }),

  reset: () => set({ ...initialState, panels: { ...initialState.panels } }),
}))

/** What the store looks like to a selector — the data, without the actions. */
export type BuilderSnapshot = Pick<BuilderState, "openFilePath" | "lease" | "panels">

/**
 * SPEC §18: "When the agent holds it, the editor is read-only with a visible
 * banner... Never a silently rejecting editor."
 *
 * The mode and the banner are therefore derived from the same fact, so the
 * editor cannot end up read-only with nothing on screen to explain it.
 */
export function selectEditorMode(state: BuilderSnapshot): "editable" | "read-only" {
  return state.lease.holder === "agent" ? "read-only" : "editable"
}

export function selectShowLeaseBanner(state: BuilderSnapshot): boolean {
  return selectEditorMode(state) === "read-only"
}

export function selectTakeoverPending(state: BuilderSnapshot): boolean {
  return state.lease.holder === "agent" && state.lease.pending_holder === "human"
}
