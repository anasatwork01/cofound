import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

/**
 * One config, two projects, because they need different environments and the
 * split keeps the node-side tests fast.
 *
 * `resolve.tsconfigPaths` replaces vite-tsconfig-paths, which Vite 8 announces
 * as redundant — the Next.js Vitest recipe still tells you to install it.
 *
 * happy-dom rather than jsdom: jsdom 30's engines are
 * `^22.22.2 || ^24.15.0 || >=26.0.0`, .tool-versions pins Node 22.18.0 and
 * .npmrc sets engine-strict=true, so pnpm refuses to install it. happy-dom is
 * also faster, and axe works correctly under it despite a stale warning in
 * vitest-axe's README.
 *
 * NOTE for anyone adding tests here: do not unit-test an async Server
 * Component. Next.js still documents them as unsupported in unit tests, and the
 * failure mode is silent — the component renders nothing rather than erroring,
 * so an assertion that something is ABSENT passes vacuously. Those belong in
 * task 1.18's end-to-end test.
 */
export default defineConfig({
  resolve: { tsconfigPaths: true },
  test: {
    projects: [
      {
        plugins: [react()],
        resolve: { tsconfigPaths: true },
        test: {
          name: "dom",
          environment: "happy-dom",
          globals: true,
          setupFiles: ["./vitest.setup.ts"],
          include: ["apps/**/*.test.tsx", "packages/ui/**/*.test.tsx"],
        },
      },
      {
        resolve: { tsconfigPaths: true },
        test: {
          name: "node",
          environment: "node",
          globals: true,
          include: ["apps/**/*.test.ts", "packages/ui/**/*.test.ts", "tests/console/**/*.test.ts"],
        },
      },
    ],
  },
})
