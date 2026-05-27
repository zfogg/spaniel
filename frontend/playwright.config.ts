import { defineConfig, devices } from '@playwright/test'

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
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    command: 'pnpm dev --host 127.0.0.1',
    url: 'http://localhost:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
