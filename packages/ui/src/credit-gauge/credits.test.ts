import { describe, expect, it } from "vitest"
import {
  RUNTIME_WARNING_PERCENT,
  formatCredits,
  isRuntimeLow,
  meterDisplay,
  type MeterReading,
} from "./credits"

const measured = (used: number, allowance: number, hold?: number): MeterReading =>
  hold === undefined
    ? { kind: "measured", used, allowance }
    : { kind: "measured", used, allowance, hold }

describe("formatCredits", () => {
  it("never returns a fraction (SPEC §16.6)", () => {
    for (const credits of [0.4, 33.333, 79.9, 1234.5678, 0.0031]) {
      expect(formatCredits(credits)).not.toMatch(/\./)
    }
  })

  it("rounds rather than truncates, so a balance is not flattered", () => {
    expect(formatCredits(33.333)).toBe("33")
    expect(formatCredits(33.5)).toBe("34")
    expect(formatCredits(0.4)).toBe("0")
  })

  it("groups thousands, because a five-figure balance is unreadable without it", () => {
    expect(formatCredits(4000)).toBe("4,000")
    expect(formatCredits(1234567)).toBe("1,234,567")
  })

  it("treats a non-number as nothing rather than painting NaN into the bar", () => {
    expect(formatCredits(Number.NaN)).toBe("0")
    expect(formatCredits(Number.POSITIVE_INFINITY)).toBe("0")
    expect(formatCredits(-5)).toBe("0")
  })
})

describe("meterDisplay", () => {
  it("splits the filled part into a settled segment and a held one", () => {
    const meter = meterDisplay({ kind: "measured", used: 2000, allowance: 4000, hold: 400 })
    expect(meter.usedPercent).toBe(50)
    expect(meter.holdPercent).toBe(10)
    expect(meter.committed).toBe(2400)
  })

  it("keeps the two segments inside the track even when the meter is overdrawn", () => {
    const meter = meterDisplay({ kind: "measured", used: 5000, allowance: 4000, hold: 900 })
    expect(meter.usedPercent).toBe(100)
    expect(meter.holdPercent).toBe(0)
    // The true figures survive for the label; only the geometry is clamped.
    expect(meter.committed).toBe(5900)
  })

  it("gives a hold that exists at least one visible percent", () => {
    // A hold rounded away is a hold the user does not believe in.
    const meter = meterDisplay({ kind: "measured", used: 0, allowance: 4000, hold: 4 })
    expect(meter.holdPercent).toBe(1)
  })

  it("does not draw a hold the label has already rounded away", () => {
    // The bar and the number beside it come from the same rounded figures, so
    // they cannot disagree about whether a hold exists.
    const meter = meterDisplay({ kind: "measured", used: 0, allowance: 4000, hold: 0.4 })
    expect(meter.hold).toBe(0)
    expect(meter.holdPercent).toBe(0)
  })

  it("draws nothing when there is nothing to draw", () => {
    const meter = meterDisplay({ kind: "measured", used: 0, allowance: 4000 })
    expect(meter.usedPercent).toBe(0)
    expect(meter.holdPercent).toBe(0)
  })

  it("survives an allowance of zero without dividing by it", () => {
    const meter = meterDisplay({ kind: "measured", used: 10, allowance: 0, hold: 2 })
    expect(meter.usedPercent).toBe(0)
    expect(meter.holdPercent).toBe(0)
    expect(meter.allowance).toBe(0)
  })

  it("rounds every number it hands the DOM", () => {
    const meter = meterDisplay({ kind: "measured", used: 33.333, allowance: 100.5, hold: 0.4 })
    for (const value of Object.values(meter)) {
      expect(Number.isInteger(value)).toBe(true)
    }
  })
})

describe("isRuntimeLow", () => {
  it("warns at exactly 80 percent, which is where SPEC §16.3's ladder starts", () => {
    expect(RUNTIME_WARNING_PERCENT).toBe(80)
    expect(isRuntimeLow(measured(80, 100))).toBe(true)
  })

  it("does not warn below the threshold, however close", () => {
    expect(isRuntimeLow(measured(79, 100))).toBe(false)
    expect(isRuntimeLow(measured(79.9, 100))).toBe(false)
  })

  it("counts holds, because a reserved credit is already spoken for", () => {
    expect(isRuntimeLow(measured(70, 100, 10))).toBe(true)
    expect(isRuntimeLow(measured(70, 100, 9))).toBe(false)
  })

  it("does not warn about a meter it cannot read", () => {
    // Silence is right here: an invented warning is worse than no gauge.
    expect(isRuntimeLow({ kind: "unknown" })).toBe(false)
  })

  it("warns on any spend against a zero allowance", () => {
    expect(isRuntimeLow(measured(1, 0))).toBe(true)
    expect(isRuntimeLow(measured(0, 0))).toBe(false)
  })

  it("is exact at the threshold for awkward denominators", () => {
    // 0.8 is not representable in binary; 2400 * 100 >= 3000 * 80 is.
    expect(isRuntimeLow(measured(2400, 3000))).toBe(true)
    expect(isRuntimeLow(measured(2399, 3000))).toBe(false)
  })
})
