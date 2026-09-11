/**
 * Registers jest-dom's matchers with TypeScript.
 *
 * `vitest.setup.ts` imports `@testing-library/jest-dom/vitest` at run time, so
 * `toBeInTheDocument` and friends work in a test — but that file is at the
 * repository root and outside this package's `tsconfig.json`, so the type
 * augmentation it carries never reaches the console's program. Without this
 * line every jest-dom assertion type-checks as an error while passing
 * perfectly, which is the worst of both.
 */
import "@testing-library/jest-dom/vitest"
