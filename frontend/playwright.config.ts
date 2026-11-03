import { defineConfig, devices } from '@playwright/test'

// Run the same browser contracts against an isolated production collector
// when qualifying its built assets; ordinary development still owns Vite.
const externalBaseURL = process.env.SPANIEL_E2E_BASE_URL
const fixturePort = Number(process.env.SPANIEL_E2E_PORT ?? 5173)
const fixtureURL = `http://127.0.0.1:${fixturePort}`

/**
 * Playwright config for the spaniel frontend.
 *
 * The dev server is owned by Playwright (webServer) so `pnpm e2e` just works
 * locally and in CI without a separate `pnpm dev` running. API calls are
 * intercepted in each spec via `page.route()`, so no Go backend is required.
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  reporter: 'list',
  retries: process.env.CI ? 2 : 1,
  // Cap workers locally so the single Vite dev server isn't overwhelmed by
  // 8 concurrent browser pages all triggering route handlers + reloads.
  // CI runners pick their own default.
  workers: process.env.CI ? undefined : 4,
  use: {
    baseURL: externalBaseURL ?? fixtureURL,
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: externalBaseURL
    ? undefined
    : {
        command: `pnpm dev --host 127.0.0.1 --port ${fixturePort} --strictPort`,
        // Mocked API tests must not export synthetic single-span traces into
        // a developer's collector. Live collector tests use its built assets.
        env: { VITE_SPANIEL_TELEMETRY: 'false' },
        url: fixtureURL,
        reuseExistingServer: !process.env.CI && !process.env.SPANIEL_E2E_PORT,
        timeout: 60_000,
        stdout: 'ignore',
        stderr: 'pipe',
      },
})
