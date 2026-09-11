/**
 * The console imports everything from here, never from a deep path.
 *
 * Two things are deliberately NOT exported:
 *
 *   - the token CSS, which is reached through the package's `exports` map
 *     (`@halyard/ui/tokens.css`) because it is a stylesheet, not a module;
 *   - anything from `./tokens/`, because `--raw-*` values are private to the
 *     token layer. A component that reaches a raw value keeps its colour when
 *     the mockup remaps the semantic one, so the swap would miscolour silently
 *     rather than fail. `tests/console/tokens-quarantine.test.ts` enforces it.
 */

export { Status } from "./status/status"
export type { StatusProps, StatusState } from "./status/status"

export { CreditGauge } from "./credit-gauge/credit-gauge"
export type { CreditGaugeProps } from "./credit-gauge/credit-gauge"

export {
  UNKNOWN_CREDITS,
  RUNTIME_WARNING_PERCENT,
  formatCredits,
  meterDisplay,
  isRuntimeLow,
} from "./credit-gauge/credits"
export type { MeterReading, MeasuredReading, MeterDisplay } from "./credit-gauge/credits"

export { TopBar } from "./chrome/top-bar"
export type { TopBarProps } from "./chrome/top-bar"
